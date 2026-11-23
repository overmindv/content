//go:build component

package component

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/store/postgres"
)

func BenchmarkMaterialSnapshotPage(b *testing.B) {
	pool, err := pgxpool.NewWithConfig(b.Context(), testDatabaseConfig(b))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(pool.Close)
	var watermark int64
	if err = pool.QueryRow(b.Context(), `
		UPDATE content_catalog_clock SET watermark = watermark + 20 WHERE singleton RETURNING watermark
	`).Scan(&watermark); err != nil {
		b.Fatal(err)
	}
	if _, err = pool.Exec(b.Context(), `
		WITH seeded AS (
			INSERT INTO content_catalog_materials (material_id, first_version)
			SELECT gen_random_uuid(), $1 - 19 FROM generate_series(1, 500) RETURNING material_id
		)
		INSERT INTO content_catalog_versions (material_id, version, revision_id, tombstone, metadata, activity_occurred_at)
		SELECT material_id, $1 - n, gen_random_uuid(), false,
		       '{"title":"Graph algorithms","type":"note","format":"markdown","topic_ids":[],"estimated_minutes":20}'::jsonb,
		       statement_timestamp()
		FROM seeded CROSS JOIN generate_series(0, 19) AS n
	`, watermark); err != nil {
		b.Fatal(err)
	}
	b.Run("distinct_history", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var boundary int64
			if err := pool.QueryRow(b.Context(), "SELECT watermark FROM content_catalog_clock WHERE singleton").Scan(&boundary); err != nil {
				b.Fatal(err)
			}
			rows, err := pool.Query(b.Context(), `
				SELECT DISTINCT ON (material_id) material_id, revision_id, version, tombstone, metadata
				FROM content_catalog_versions WHERE material_id > $1 AND version <= $2
				ORDER BY material_id, version DESC LIMIT 51
			`, uuid.Nil.String(), boundary)
			if err != nil {
				b.Fatal(err)
			}
			entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.MaterialVersion, error) {
				var value domain.MaterialVersion
				scanErr := row.Scan(&value.MaterialID, &value.RevisionID, &value.Version, &value.State.Tombstone, &value.State.Metadata)

				return value, scanErr
			})
			if err != nil || len(entries) != 51 {
				b.Fatal("expected a full snapshot page", err)
			}
		}
	})
	b.Run("indexed_material_page", func(b *testing.B) {
		b.ReportAllocs()
		store := postgres.NewContentItemStore(pool)
		for b.Loop() {
			page, err := store.SnapshotMaterials(b.Context(), domain.MaterialSnapshotInput{PageSize: 50})
			if err != nil || len(page.Entries) != 50 || page.NextAfterID == "" {
				b.Fatal("expected a full snapshot page", err)
			}
		}
	})
}
