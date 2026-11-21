//go:build component

package component

import (
	"errors"
	"testing"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

func TestPublishedMaterialNeverReadsWorkingRevision(t *testing.T) {
	ctx := t.Context()
	store, item := seedRevisionHistory(t, 1, "# First publication")
	items := service.NewContentItemService(store, nil)
	if _, err := items.GetPublished(ctx, item.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("draft accessible in published view: %v", err)
	}
	state := domain.ChangeContentItemStateInput{ID: item.ID, ActorID: item.CreatedBy, ExpectedRevisionID: item.CurrentRevisionID}
	item, err := store.ChangeState(ctx, state, domain.ContentStatusPublished)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID: item.ID, CreatedBy: item.CreatedBy, ExpectedRevisionID: item.CurrentRevisionID,
		Format: domain.ContentFormatMarkdown, Source: "# Unpublished edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := items.Get(ctx, item.ID, item.CreatedBy)
	if err != nil || owner.CurrentRevisionID != draft.ID {
		t.Errorf("editor no longer sees working revision: %v", err)
	}
	published, err := items.GetPublished(ctx, item.ID)
	if err != nil || published.CurrentRevision == nil || published.CurrentRevision.ID != item.PublishedRevisionID ||
		published.CurrentRevision.Source == draft.Source {
		t.Errorf("published read did not return the published revision: %v", err)
	}
	state.ExpectedRevisionID = draft.ID
	if _, err := store.ChangeState(ctx, state, domain.ContentStatusArchived); err != nil {
		t.Fatal(err)
	}
	if _, err := items.GetPublished(ctx, item.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("archive accessible in published view: %v", err)
	}
}

func BenchmarkPublishedMaterial(b *testing.B) {
	store, item := seedRevisionHistory(b, 1, "# First publication")
	state := domain.ChangeContentItemStateInput{ID: item.ID, ActorID: item.CreatedBy, ExpectedRevisionID: item.CurrentRevisionID}
	if _, err := store.ChangeState(b.Context(), state, domain.ContentStatusPublished); err != nil {
		b.Fatal(err)
	}
	items := service.NewContentItemService(store, nil)
	for _, actor := range []string{item.CreatedBy, ""} {
		label := "owner"
		if actor == "" {
			label = "published"
		}
		b.Run(label, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var err error
				if actor == "" {
					_, err = items.GetPublished(b.Context(), item.ID)
				} else {
					_, err = items.Get(b.Context(), item.ID, actor)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
