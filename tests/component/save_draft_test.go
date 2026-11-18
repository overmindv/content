//go:build component

package component

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

func TestSaveDraftMetadataDoesNotOverwriteAnotherRevision(t *testing.T) {
	f := newActivityFixture(t)
	item := f.create(t)
	topic := uuid.NewString()
	input := domain.CreateContentRevisionInput{
		ContentItemID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, CreatedBy: f.owner,
		Format: domain.ContentFormatMarkdown, Source: "    code\n\n",
		DraftMetadata: &domain.DraftMetadata{Title: "New title", TopicIDs: []string{topic}},
	}
	revision, err := f.store.CreateRevision(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	stale := input
	stale.Source = "# Stale text"
	stale.DraftMetadata = &domain.DraftMetadata{Title: "Stale title"}
	if _, err := f.store.CreateRevision(f.ctx, stale); !errors.Is(err, service.ErrRevisionConflict) {
		t.Errorf("stale save: %v", err)
	}
	foreign := input
	foreign.ExpectedRevisionID, foreign.CreatedBy = revision.ID, uuid.NewString()
	if _, err := f.store.CreateRevision(f.ctx, foreign); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("save by non-owner: %v", err)
	}
	saved, err := f.store.Get(f.ctx, item.ID, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title != input.DraftMetadata.Title || saved.CurrentRevision == nil ||
		saved.CurrentRevision.ID != revision.ID || saved.CurrentRevision.Source != input.Source ||
		saved.Learning == nil || saved.Learning.EstimatedMinutes != 17 ||
		len(saved.Learning.TopicIDs) != 1 || saved.Learning.TopicIDs[0] != topic {
		t.Errorf("saved draft = %+v, revision = %+v, learning = %+v", saved, saved.CurrentRevision, saved.Learning)
	}

	input.ExpectedRevisionID = revision.ID
	input.DraftMetadata = &domain.DraftMetadata{Title: "No topics"}
	revision, err = f.store.CreateRevision(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	published := f.state(t, domain.ContentItem{ID: item.ID, CurrentRevisionID: revision.ID}, domain.ContentStatusPublished)
	if published.Learning == nil || len(published.Learning.TopicIDs) != 0 || published.Learning.EstimatedMinutes != 17 {
		t.Errorf("metadata after clearing topics = %+v", published.Learning)
	}
	input.ExpectedRevisionID = revision.ID
	input.DraftMetadata.Title = "Not for publication"
	if _, err := f.store.CreateRevision(f.ctx, input); !errors.Is(err, service.ErrInvalidState) {
		t.Errorf("edit metadata of published material: %v", err)
	}
	read, err := f.store.Get(f.ctx, item.ID, "")
	if err != nil || read.Title != published.Title || read.PublishedRevisionID != revision.ID {
		t.Errorf("published material changed: title = %q, revision = %q, error = %v", read.Title, read.PublishedRevisionID, err)
	}
}
