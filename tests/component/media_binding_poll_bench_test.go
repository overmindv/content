//go:build component

package component

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/store/postgres"
)

func BenchmarkMediaBindingIdle(b *testing.B) {
	ctx := b.Context()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(b))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `
		INSERT INTO content_media_bindings (revision_id, material_id, actor_user_id, file_ids, applied_version)
		SELECT gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), ARRAY[gen_random_uuid()], 1
		FROM generate_series(1, 10000)`); err != nil {
		b.Fatal(err)
	}
	store := postgres.NewContentItemStore(pool, postgres.WithManagedAttachments())
	b.ReportAllocs()
	for b.Loop() {
		if _, found, claimErr := store.ClaimMediaBinding(ctx); found || claimErr != nil {
			b.Fatal("expected an idle binding queue", found, claimErr)
		}
	}
}
