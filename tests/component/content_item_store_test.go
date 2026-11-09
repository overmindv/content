//go:build component

package component

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/tests/builders"
)

func TestContentItemStoreCRUD(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN is required")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Errorf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	// События контента пишутся в outbox атомарно с бизнес-изменением (блок Б аудита).
	store := postgres.NewContentItemStore(pool,
		postgres.WithOutbox(true, "content-test-topic"))

	created, err := store.Create(ctx, builders.ContentItem())
	if err != nil {
		t.Errorf("Create() error = %v", err)
	}

	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	if created.CurrentRevision.Revision != 1 {
		t.Errorf("expected revision 1, got %d", created.CurrentRevision.Revision)
	}
	if len(created.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(created.Tags))
	}

	// Создание айтема атомарно положило событие в outbox.
	if n := pendingOutboxRows(t, ctx, pool, created.ID); n < 1 {
		t.Fatalf("expected at least 1 outbox row for created item, got %d", n)
	}

	fetched, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}

	if fetched.Title != created.Title {
		t.Errorf("expected title %q, got %q", created.Title, fetched.Title)
	}

	updated, err := store.Update(ctx, domain.UpdateContentItemInput{
		ID:          created.ID,
		Type:        domain.ContentTypeArticle,
		Status:      domain.ContentStatusPublished,
		Title:       "Builder content item updated",
		Slug:        "builder-content-item-updated",
		Description: "updated by component test",
	})
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	if updated.Status != domain.ContentStatusPublished {
		t.Errorf("expected updated status published, got %q", updated.Status)
	}
	if updated.PublishedRevisionID == "" {
		t.Error("expected published revision id")
	}

	revision, err := store.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID: created.ID,
		Format:        domain.ContentFormatTypst,
		Source:        "= Builder content item",
		Message:       "typst revision",
	})
	if err != nil {
		t.Errorf("CreateRevision() error = %v", err)
	}

	if revision.Revision != 2 {
		t.Errorf("expected revision 2, got %d", revision.Revision)
	}

	revisions, err := store.ListRevisions(ctx, created.ID)
	if err != nil {
		t.Errorf("ListRevisions() error = %v", err)
	}
	if len(revisions) != 2 {
		t.Errorf("expected 2 revisions, got %d", len(revisions))
	}

	items, err := store.List(ctx)
	if err != nil {
		t.Errorf("List() error = %v", err)
	}

	if len(items) == 0 {
		t.Fatal("expected non-empty item list")
	}

	deleted, err := store.Delete(ctx, created.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	if deleted.ID != created.ID {
		t.Errorf("expected deleted id %q, got %q", created.ID, deleted.ID)
	}

	// Последовательность create/update/delete/revision поставила в outbox 4 события.
	if n := pendingOutboxRows(t, ctx, pool, created.ID); n < 4 {
		t.Errorf("expected at least 4 outbox rows for item, got %d", n)
	}
}

// pendingOutboxRows считает ожидающие outbox-события контент-айтема.
func pendingOutboxRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE message_key = $1 AND status = 'pending'`, key).Scan(&n); err != nil {
		t.Fatalf("query outbox: %v", err)
	}

	return n
}
