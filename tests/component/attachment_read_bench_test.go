//go:build component

package component

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func BenchmarkAttachmentRead(b *testing.B) {
	store, item := seedRevisionHistory(b, 1000, "# With attachments")
	pool, err := pgxpool.NewWithConfig(b.Context(), testDatabaseConfig(b))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(pool.Close)
	if _, err = pool.Exec(b.Context(), `
		INSERT INTO content_assets (id, content_item_id, revision_id, asset_id, kind, position)
		SELECT gen_random_uuid(), r.content_item_id, r.id, gen_random_uuid(), 'attachment', n
		FROM content_revisions AS r CROSS JOIN generate_series(1, 8) AS n WHERE r.content_item_id = $1
	`, item.ID); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	var returned int
	for b.Loop() {
		view, err := store.Get(b.Context(), item.ID, item.CreatedBy)
		if err != nil {
			b.Fatal(err)
		}
		returned = len(view.Assets)
	}
	b.ReportMetric(float64(returned), "assets/op")
}
