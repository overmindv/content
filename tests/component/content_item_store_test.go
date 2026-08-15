//go:build component

package component

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/tests/builders"
)

func TestContentItemStoreCRUD(t *testing.T) {
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN is required")
	}

	db, err := postgres.Open(postgres.Config{
		DSN:             dsn,
		MaxOpenConns:    5,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Errorf("Open() error = %v", err)
	}
	defer db.Close()

	store := postgres.NewContentItemStore(db)
	ctx := context.Background()

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
}
