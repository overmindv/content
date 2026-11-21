//go:build component

package component

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/tests/builders"
)

func TestSaveDraftDoesNotOverwriteAnotherRevision(t *testing.T) {
	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := postgres.NewContentItemStore(pool, postgres.WithOutbox(true, "content-test"))
	create := builders.ContentItem()
	create.Slug += "-" + uuid.NewString()
	create.CreatedBy = uuid.NewString()
	create.Learning = &domain.LearningMetadata{EstimatedMinutes: 17, TopicIDs: []string{uuid.NewString()}}
	item, err := store.Create(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	topic := uuid.NewString()
	files := &domain.AttachmentSet{Assets: []domain.CreateContentAssetInput{
		{AssetID: uuid.NewString(), Kind: domain.AssetKindAttachment},
	}}
	input := domain.CreateContentRevisionInput{
		ContentItemID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, CreatedBy: create.CreatedBy,
		Format: domain.ContentFormatMarkdown, Source: "    code\n\n", Attachments: files,
		DraftMetadata: &domain.DraftMetadata{Title: "New title", TopicIDs: []string{topic}},
	}
	revision, err := store.CreateRevision(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	stale := input
	stale.Source, stale.DraftMetadata, stale.Attachments = "# Stale", &domain.DraftMetadata{Title: "Stale"}, &domain.AttachmentSet{}
	if _, err = store.CreateRevision(ctx, stale); !errors.Is(err, service.ErrRevisionConflict) {
		t.Error("stale save", err)
	}
	foreign := input
	foreign.ExpectedRevisionID, foreign.CreatedBy = revision.ID, uuid.NewString()
	if _, err = store.CreateRevision(ctx, foreign); !errors.Is(err, service.ErrNotFound) {
		t.Error("save by non-owner", err)
	}
	saved, err := store.Get(ctx, item.ID, create.CreatedBy)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title != input.DraftMetadata.Title || saved.CurrentRevision == nil || saved.CurrentRevision.ID != revision.ID ||
		saved.CurrentRevision.Source != input.Source || saved.Learning == nil || saved.Learning.EstimatedMinutes != 17 ||
		len(saved.Learning.TopicIDs) != 1 || saved.Learning.TopicIDs[0] != topic ||
		len(saved.Assets) != 1 || saved.Assets[0].AssetID != files.Assets[0].AssetID {
		t.Error("draft metadata, source and attachments were not saved together")
	}
	if n := pendingOutboxRows(t, ctx, pool, item.ID); n != 3 {
		t.Error("successful create and save must be the only events", n)
	}

	input.ExpectedRevisionID = revision.ID
	input.DraftMetadata = &domain.DraftMetadata{Title: "No topics"}
	revision, err = store.CreateRevision(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	state := domain.ChangeContentItemStateInput{ID: item.ID, ExpectedRevisionID: revision.ID, ActorID: create.CreatedBy}
	published, err := store.ChangeState(ctx, state, domain.ContentStatusPublished)
	if err != nil || published.Learning == nil || len(published.Learning.TopicIDs) != 0 || published.Learning.EstimatedMinutes != 17 {
		t.Fatal("clearing topics changed unrelated fields", err)
	}
	input.ExpectedRevisionID = revision.ID
	input.DraftMetadata.Title = "Not for publication"
	if _, err = store.CreateRevision(ctx, input); !errors.Is(err, service.ErrInvalidState) {
		t.Error("accepted metadata edit of published material", err)
	}
	read, err := store.Get(ctx, item.ID, "")
	if err != nil || read.Title != published.Title || read.PublishedRevisionID != revision.ID {
		t.Error("published material changed after rejected save", err)
	}
}

func BenchmarkSaveDraft(b *testing.B) {
	for _, name := range []string{"revision", "with_metadata"} {
		b.Run(name, func(b *testing.B) {
			store, item := seedRevisionHistory(b, 100, "# Draft")
			input := domain.CreateContentRevisionInput{
				ContentItemID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, CreatedBy: item.CreatedBy,
				Format: domain.ContentFormatMarkdown, Source: strings.Repeat("text\n", 800),
			}
			if name == "with_metadata" {
				input.DraftMetadata = &domain.DraftMetadata{Title: "New title", TopicIDs: []string{uuid.NewString(), uuid.NewString()}}
			}
			b.ReportAllocs()
			for b.Loop() {
				saved, err := store.CreateRevision(b.Context(), input)
				if err != nil {
					b.Fatal(err)
				}
				input.ExpectedRevisionID = saved.ID
			}
		})
	}
}
