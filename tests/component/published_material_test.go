//go:build component

package component

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

func TestPublishedMaterialNeverReadsWorkingRevision(t *testing.T) {
	f := newActivityFixture(t)
	items := service.NewContentItemService(f.store, nil)
	item := f.create(t)
	if _, err := items.GetPublished(f.ctx, item.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("draft accessible in published view: %v", err)
	}
	item = f.state(t, item, domain.ContentStatusPublished)
	draft, err := f.store.CreateRevision(f.ctx, domain.CreateContentRevisionInput{
		ContentItemID: item.ID, CreatedBy: f.owner, ExpectedRevisionID: item.CurrentRevisionID,
		Format: domain.ContentFormatMarkdown, Source: "# Unpublished edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := items.Get(f.ctx, item.ID, f.owner)
	if err != nil || owner.CurrentRevisionID != draft.ID {
		t.Errorf("editor no longer sees working revision: %v", err)
	}
	published, err := items.GetPublished(f.ctx, item.ID)
	if err != nil || published.CurrentRevision == nil || published.CurrentRevision.ID != item.PublishedRevisionID ||
		published.CurrentRevision.Source == draft.Source {
		t.Errorf("published read did not return the published revision: %v", err)
	}
	input := domain.SetMaterialModerationInput{
		ActorID: uuid.NewString(), Reason: "fixture reason",
		MaterialModeration: domain.MaterialModeration{MaterialID: item.ID, PublishedRevisionID: item.PublishedRevisionID, Hidden: true},
	}
	hidden, err := f.store.SetMaterialModeration(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := items.GetPublished(f.ctx, item.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("hidden item accessible in published view: %v", err)
	}
	input.Hidden, input.Version = false, hidden.Version
	if _, err := f.store.SetMaterialModeration(f.ctx, input); err != nil {
		t.Fatal(err)
	}
	item.CurrentRevisionID = draft.ID
	f.state(t, item, domain.ContentStatusArchived)
	if _, err := items.GetPublished(f.ctx, item.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("archive accessible in published view: %v", err)
	}
}
