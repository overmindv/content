//go:build component

package component

import (
	"context"
	"errors"
	"os"
	"strings"
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
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(t))
	if err != nil {
		t.Fatal("could not open test database")
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
		Status:      domain.ContentStatusDraft,
		Title:       "Builder content item updated",
		Slug:        "builder-content-item-updated",
		Description: "updated by component test",
		UpdatedBy:   input.CreatedBy,
	})
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	if updated.Status != domain.ContentStatusDraft || updated.PublishedRevisionID != "" {
		t.Errorf("metadata update published the draft: %v", updated.Status)
	}
	published, err := store.ChangeState(ctx, domain.ChangeContentItemStateInput{
		ID: created.ID, ExpectedRevisionID: created.CurrentRevisionID, ActorID: input.CreatedBy,
	}, domain.ContentStatusPublished)
	if err != nil || published.PublishedRevisionID != created.CurrentRevisionID {
		t.Errorf("publish did not set the published revision: %v", err)
	}

	revision, err := store.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID:      created.ID,
		ExpectedRevisionID: created.CurrentRevisionID,
		Format:             domain.ContentFormatTypst,
		Source:             "= Builder content item",
		Message:            "typst revision",
		CreatedBy:          input.CreatedBy,
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

	if n := pendingOutboxRows(t, ctx, pool, created.ID); n != 5 {
		t.Errorf("expected 5 outbox rows for item, got %d", n)
	}
}

func TestContentItemStoreAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := testDatabaseConfig(t)
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("could not open test database")
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
		ID: item.ID, Type: item.Type, Status: domain.ContentStatusDraft,
		Title: item.Title, Slug: item.Slug, UpdatedBy: reader,
	}
	changed := domain.CreateContentRevisionInput{
		ContentItemID: item.ID, ExpectedRevisionID: item.CurrentRevisionID,
		Format: domain.ContentFormatMarkdown, Source: "# Unpublished", CreatedBy: reader,
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

	state := domain.ChangeContentItemStateInput{ID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, ActorID: reader}
	_, err = store.ChangeState(ctx, state, domain.ContentStatusPublished)
	assertHidden(err)
	_, err = store.ChangeState(ctx, state, domain.ContentStatusArchived)
	assertHidden(err)
	state.ActorID = owner
	published, err := store.ChangeState(ctx, state, domain.ContentStatusPublished)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusPublished); err != nil {
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

	_, err = store.CreateRevision(ctx, changed)
	if !errors.Is(err, service.ErrRevisionConflict) {
		t.Errorf("stale revision: %v", err)
	}
	for _, target := range []domain.ContentStatus{domain.ContentStatusPublished, domain.ContentStatusArchived} {
		_, err = store.ChangeState(ctx, state, target)
		if !errors.Is(err, service.ErrRevisionConflict) {
			t.Errorf("stale %s: %v", target, err)
		}
	}
	update.UpdatedBy = owner
	update.Title = "Unpublished title"
	_, err = store.Update(ctx, update)
	if !errors.Is(err, service.ErrInvalidState) {
		t.Errorf("published metadata update: %v", err)
	}
	view, err = store.Get(ctx, item.ID, reader)
	if err != nil || view.Title != published.Title {
		t.Fatalf("rejected edits changed the public view: %v", err)
	}
	assertPublishedView(view)

	state.ExpectedRevisionID = next.ID
	republished, err := store.ChangeState(ctx, state, domain.ContentStatusPublished)
	if err != nil || republished.PublishedRevisionID != next.ID {
		t.Fatalf("publish with current revision: %v", err)
	}
	view, err = store.Get(ctx, item.ID, reader)
	if err != nil || view.CurrentRevision == nil || view.CurrentRevision.Source != changed.Source {
		t.Errorf("new published revision not visible: %v", err)
	}
	archived, err := store.ChangeState(ctx, state, domain.ContentStatusArchived)
	if err != nil || archived.Status != domain.ContentStatusArchived || archived.ArchivedAt == nil {
		t.Fatalf("archive with current revision: %v", err)
	}
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusArchived); err != nil {
		t.Fatal(err)
	}
	_, err = store.Get(ctx, item.ID, reader)
	assertHidden(err)
	_, err = store.ChangeState(ctx, state, domain.ContentStatusPublished)
	if !errors.Is(err, service.ErrInvalidState) {
		t.Errorf("publish archived item: %v", err)
	}
	changed.ExpectedRevisionID = next.ID
	_, err = store.CreateRevision(ctx, changed)
	if !errors.Is(err, service.ErrInvalidState) {
		t.Errorf("write archived item: %v", err)
	}
	if n := pendingOutboxRows(t, ctx, pool, item.ID); n != 5 {
		t.Errorf("outbox rows after retries and conflicts = %d, want 5", n)
	}
	if _, err = store.Delete(ctx, item.ID, owner); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentContentRevisionsRequireCurrentBase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(t))
	if err != nil {
		t.Fatal("could not open test database")
	}
	defer pool.Close()
	store := postgres.NewContentItemStore(pool, postgres.WithOutbox(true, "content-test-topic"))
	input := builders.ContentItem()
	input.Slug += "-" + uuid.NewString()
	input.CreatedBy = uuid.NewString()
	item, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, source := range []string{"# First edit", "# Second edit"} {
		go func() {
			<-start
			_, err := store.CreateRevision(ctx, domain.CreateContentRevisionInput{
				ContentItemID: item.ID, ExpectedRevisionID: item.CurrentRevisionID,
				Format: domain.ContentFormatMarkdown, Source: source, CreatedBy: input.CreatedBy,
			})
			results <- err
		}()
	}
	close(start)
	var saved, rejected int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			saved++
		case errors.Is(err, service.ErrRevisionConflict):
			rejected++
		default:
			t.Errorf("unexpected result: %v", err)
		}
	}
	if saved != 1 || rejected != 1 {
		t.Errorf("saved = %d, conflicted = %d, want one of each", saved, rejected)
	}
	if n := pendingOutboxRows(t, ctx, pool, item.ID); n != 2 {
		t.Errorf("outbox rows after concurrent edits = %d, want 2", n)
	}
	if _, err = store.Delete(ctx, item.ID, input.CreatedBy); err != nil {
		t.Fatal(err)
	}
}

func testDatabaseConfig(t testing.TB) *pgxpool.Config {
	t.Helper()
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN is required")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		t.Fatal("COMPONENT_TEST_DSN must point to a dedicated database ending in _test")
	}
	return config
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
