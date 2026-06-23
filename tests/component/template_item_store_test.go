//go:build component

package component

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/overmindv/bumblebee/internal/pkg/domain"
	"github.com/overmindv/bumblebee/internal/pkg/store/postgres"
	"github.com/overmindv/bumblebee/tests/builders"
)

func TestTemplateItemStoreCRUD(t *testing.T) {
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

	store := postgres.NewTemplateItemStore(db)
	ctx := context.Background()

	created, err := store.Create(ctx, builders.TemplateItem())
	if err != nil {
		t.Errorf("Create() error = %v", err)
	}

	fetched, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}

	if fetched.Name != created.Name {
		t.Errorf("expected name %q, got %q", created.Name, fetched.Name)
	}

	updated, err := store.Update(ctx, domain.UpdateTemplateItemInput{
		ID:          created.ID,
		Name:        "builder-item-updated",
		Description: "updated by component test",
		Status:      "active",
	})
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	if updated.Status != "active" {
		t.Errorf("expected updated status active, got %q", updated.Status)
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
