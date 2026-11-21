//go:build component

package component

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/internal/pkg/validator"
	"github.com/overmindv/content/tests/builders"
)

func TestPublicationRejectsUnsupportedSavedRevisions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(t))
	if err != nil {
		t.Fatal("could not open test database")
	}
	defer pool.Close()
	store := postgres.NewContentItemStore(pool, postgres.WithOutbox(true, "content-test-topic"))
	for _, tt := range []struct {
		name       string
		format     domain.ContentFormat
		source     string
		assetCount int
	}{
		{name: "typst", format: domain.ContentFormatTypst, source: "= Typst"},
		{name: "legacy html", format: domain.ContentFormatMarkdown, source: "<p>Example</p>"},
		{name: "legacy large source", format: domain.ContentFormatMarkdown, source: strings.Repeat("x", validator.MaxSourceBytes+1)},
		{name: "legacy attachment count", format: domain.ContentFormatMarkdown, source: "# Attachments", assetCount: validator.MaxAssets + 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := builders.ContentItem()
			input.Slug += "-" + uuid.NewString()
			input.CreatedBy = uuid.NewString()
			input.Format = tt.format
			input.Source = tt.source
			for range tt.assetCount {
				input.Assets = append(input.Assets, domain.CreateContentAssetInput{AssetID: uuid.NewString(), Kind: domain.AssetKindAttachment})
			}
			item, err := store.Create(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := store.Delete(ctx, item.ID, input.CreatedBy); err != nil {
					t.Error(err)
				}
			}()
			state := domain.ChangeContentItemStateInput{ID: item.ID, ActorID: input.CreatedBy, ExpectedRevisionID: item.CurrentRevisionID}
			if _, err = store.ChangeState(ctx, state, domain.ContentStatusPublished); !validator.IsValidationError(err) {
				t.Errorf("publish error = %v, want validation error", err)
			}
			if _, err = store.Get(ctx, item.ID, uuid.NewString()); !errors.Is(err, service.ErrNotFound) {
				t.Errorf("reader Get error = %v, want NotFound", err)
			}
			if n := pendingOutboxRows(t, ctx, pool, item.ID); n != 1 {
				t.Errorf("outbox rows after rejected publication = %d, want 1", n)
			}
			if _, err = store.ChangeState(ctx, state, domain.ContentStatusArchived); err != nil {
				t.Error(err)
			}
		})
	}
}
