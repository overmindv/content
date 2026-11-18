//go:build component

package component

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
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

	input := builders.ContentItem()
	input.CreatedBy = uuid.NewString()
	created, err := store.Create(ctx, input)
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

	fetched, err := store.Get(ctx, created.ID, input.CreatedBy)
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
		UpdatedBy:   input.CreatedBy,
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
		CreatedBy:     input.CreatedBy,
	})
	if err != nil {
		t.Errorf("CreateRevision() error = %v", err)
	}

	if revision.Revision != 2 {
		t.Errorf("expected revision 2, got %d", revision.Revision)
	}

	revisions, err := store.ListRevisions(ctx, created.ID, input.CreatedBy)
	if err != nil {
		t.Errorf("ListRevisions() error = %v", err)
	}
	if len(revisions) != 2 {
		t.Errorf("expected 2 revisions, got %d", len(revisions))
	}

	items, err := store.List(ctx, input.CreatedBy)
	if err != nil {
		t.Errorf("List() error = %v", err)
	}

	if len(items) == 0 {
		t.Fatal("expected non-empty item list")
	}

	deleted, err := store.Delete(ctx, created.ID, input.CreatedBy)
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

func TestContentItemStoreAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN is required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := postgres.NewContentItemStore(pool, postgres.WithOutbox(true, "content-test-topic"))
	owner, reader := uuid.NewString(), uuid.NewString()
	input := builders.ContentItem()
	input.Slug += "-" + uuid.NewString()
	input.CreatedBy = owner
	assetID := uuid.NewString()
	input.Assets = []domain.CreateContentAssetInput{{AssetID: assetID, Kind: domain.AssetKindAttachment}}
	item, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	update := domain.UpdateContentItemInput{
		ID: item.ID, Type: item.Type, Status: domain.ContentStatusPublished,
		Title: item.Title, Slug: item.Slug, UpdatedBy: reader,
	}
	changed := domain.CreateContentRevisionInput{
		ContentItemID: item.ID, Format: domain.ContentFormatMarkdown, Source: "# Unpublished", CreatedBy: reader,
	}
	assertHidden := func(err error) {
		t.Helper()
		if !errors.Is(err, service.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	}
	_, err = store.Get(ctx, item.ID, reader)
	assertHidden(err)
	items, err := store.List(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range items {
		if other.ID == item.ID {
			t.Error("draft returned to another user in List")
		}
	}
	_, err = store.ListRevisions(ctx, item.ID, reader)
	assertHidden(err)
	_, err = store.Update(ctx, update)
	assertHidden(err)
	_, err = store.CreateRevision(ctx, changed)
	assertHidden(err)
	_, err = store.Delete(ctx, item.ID, reader)
	assertHidden(err)
	if n := pendingOutboxRows(t, ctx, pool, item.ID); n != 1 {
		t.Errorf("outbox rows after rejected writes = %d, want 1", n)
	}
	owned, err := store.Get(ctx, item.ID, owner)
	if err != nil || owned.CurrentRevisionID != item.CurrentRevisionID || owned.Status != domain.ContentStatusDraft {
		t.Errorf("owner draft missing or changed: %v", err)
	}

	update.UpdatedBy = owner
	published, err := store.Update(ctx, update)
	if err != nil {
		t.Fatal(err)
	}
	changed.CreatedBy = owner
	next, err := store.CreateRevision(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO content_assets (id, content_item_id, revision_id, asset_id, kind)
		VALUES ($1, $2, $3, $4, 'attachment'), ($5, $2, NULL, $6, 'attachment')`,
		uuid.NewString(), item.ID, next.ID, uuid.NewString(), uuid.NewString(), uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	assertPublishedView := func(view domain.ContentItem) {
		t.Helper()
		if view.CurrentRevisionID != published.PublishedRevisionID || view.CurrentRevision == nil || view.CurrentRevision.Source != input.Source {
			t.Error("public view does not point to the published revision")
		}
		if len(view.Assets) != 1 || view.Assets[0].AssetID != assetID {
			t.Error("public view must contain only published assets")
		}
	}
	view, err := store.Get(ctx, item.ID, reader)
	if err != nil {
		t.Fatal(err)
	}
	assertPublishedView(view)
	items, err = store.List(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, public := range items {
		if public.ID == item.ID {
			found = true
			assertPublishedView(public)
		}
	}
	if !found {
		t.Error("published item missing from List")
	}
	owned, err = store.Get(ctx, item.ID, owner)
	if err != nil || owned.CurrentRevisionID != next.ID || len(owned.Assets) != 3 {
		t.Errorf("owner did not receive the unpublished revision and assets: %v", err)
	}
	_, err = store.ListRevisions(ctx, item.ID, reader)
	assertHidden(err)
	_, err = store.Delete(ctx, item.ID, reader)
	assertHidden(err)
	if _, err = store.Delete(ctx, item.ID, owner); err != nil {
		t.Fatal(err)
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
