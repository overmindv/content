//go:build component

package component

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
)

func BenchmarkPublishedAttachmentMembership(b *testing.B) {
	store, item := seedRevisionHistory(b, 1000, "# Published material\n"+strings.Repeat("paragraph ", 2000))
	state := domain.ChangeContentItemStateInput{ID: item.ID, ActorID: item.CreatedBy, ExpectedRevisionID: item.CurrentRevisionID}
	if _, err := store.ChangeState(b.Context(), state, domain.ContentStatusPublished); err != nil {
		b.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(b.Context(), testDatabaseConfig(b))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(pool.Close)
	if _, err := pool.Exec(b.Context(), `
		INSERT INTO content_assets (id, content_item_id, revision_id, asset_id, kind, position)
		SELECT gen_random_uuid(), r.content_item_id, r.id, gen_random_uuid(), 'attachment', n
		FROM content_revisions r CROSS JOIN generate_series(1, 8) n WHERE r.content_item_id = $1`, item.ID); err != nil {
		b.Fatal(err)
	}
	ref := domain.PublishedAttachmentRef{MaterialID: item.ID, RevisionID: item.CurrentRevisionID}
	if err := pool.QueryRow(b.Context(), `
		SELECT asset_id FROM content_assets WHERE content_item_id = $1 AND revision_id = $2 AND position = 8
	`, ref.MaterialID, ref.RevisionID).Scan(&ref.FileID); err != nil {
		b.Fatal(err)
	}
	b.Run("full_published_read", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			view, err := store.Get(b.Context(), ref.MaterialID, "")
			if err != nil {
				b.Fatal(err)
			}
			if !hasPublishedAsset(view, ref) {
				b.Fatal("expected a file in the publication")
			}
		}
	})
	b.Run("membership", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := store.CheckPublishedAttachment(b.Context(), ref); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func hasPublishedAsset(item domain.ContentItem, ref domain.PublishedAttachmentRef) bool {
	if item.PublishedRevisionID == ref.RevisionID {
		for _, asset := range item.Assets {
			if asset.AssetID == ref.FileID {
				return true
			}
		}
	}

	return false
}
