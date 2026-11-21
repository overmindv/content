//go:build component

package component

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/tests/builders"
)

func TestRevisionHistoryUsesOwnerAndCursor(t *testing.T) {
	ctx := t.Context()
	store, material := seedRevisionHistory(t, 5, "# Private history")
	items := service.NewContentItemService(store, nil)
	input := domain.RevisionHistoryInput{ContentItemID: material.ID, ActorID: uuid.NewString(), PageSize: 2}
	if _, _, err := items.RevisionHistory(ctx, input); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("reader accessed unpublished history: %v", err)
	}
	input.ActorID = material.CreatedBy
	var numbers []int32
	for range 3 {
		page, next, err := items.RevisionHistory(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		for _, revision := range page {
			numbers = append(numbers, revision.Revision)
			if len(revision.Message) > 256 {
				t.Error("history contains unbounded metadata")
			}
		}
		input.BeforeRevision = next
	}
	if input.BeforeRevision != 0 || !slices.Equal(numbers, []int32{5, 4, 3, 2, 1}) {
		t.Errorf("unexpected history across pages: %v, cursor %d", numbers, input.BeforeRevision)
	}
	input.BeforeRevision = 1
	page, cursor, err := items.RevisionHistory(ctx, input)
	if err != nil || cursor != 0 || len(page) != 0 {
		t.Errorf("expected empty final page for owner: %d items, cursor %d, err=%v", len(page), cursor, err)
	}
}

func BenchmarkRevisionHistory(b *testing.B) {
	store, material := seedRevisionHistory(b, 250, strings.Repeat("Revision contents.\n", 1024))
	input := domain.RevisionHistoryInput{ContentItemID: material.ID, ActorID: material.CreatedBy, PageSize: 20}
	b.Run("full_revisions", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if revisions, err := store.ListRevisions(b.Context(), material.ID, material.CreatedBy); err != nil || len(revisions) != 250 {
				b.Fatal("expected 250 revisions", err)
			}
		}
	})
	b.Run("history_page", func(b *testing.B) {
		items := service.NewContentItemService(store, nil)
		b.ReportAllocs()
		for b.Loop() {
			if page, next, err := items.RevisionHistory(b.Context(), input); err != nil || len(page) != 20 || next == 0 {
				b.Fatal("expected a page and cursor", err)
			}
		}
	})
}

func seedRevisionHistory(t testing.TB, count int, source string) (*postgres.ContentItemStore, domain.ContentItem) {
	t.Helper()
	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := postgres.NewContentItemStore(pool)
	input := builders.ContentItem()
	input.Slug += "-" + uuid.NewString()
	input.CreatedBy = uuid.NewString()
	input.Source = source
	item, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO content_revisions(id,content_item_id,revision,format,source,source_hash,message,created_by)
		SELECT gen_random_uuid(),$1,rev,'markdown',$2,'fixture',repeat('x',300),$3
		FROM generate_series(2,$4::int) rev`, item.ID, source, input.CreatedBy, count); err != nil {
		t.Fatal(err)
	}
	return store, item
}
