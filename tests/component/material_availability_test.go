//go:build component

package component

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

func TestMaterialAvailabilityUsesCurrentPublicRevision(t *testing.T) {
	f := newActivityFixture(t)
	items := service.NewContentItemService(f.store, nil)
	first := f.state(t, f.create(t), domain.ContentStatusPublished)
	second := f.state(t, f.create(t), domain.ContentStatusPublished)
	draft := f.create(t)
	working, err := f.store.CreateRevision(f.ctx, domain.CreateContentRevisionInput{
		ContentItemID: first.ID, ExpectedRevisionID: first.CurrentRevisionID, CreatedBy: f.owner,
		Format: domain.ContentFormatMarkdown, Source: "# Working revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	a := domain.MaterialVersionKey{MaterialID: first.ID, RevisionID: first.PublishedRevisionID}
	b := domain.MaterialVersionKey{MaterialID: second.ID, RevisionID: second.PublishedRevisionID}
	check := func(keys, want []domain.MaterialVersionKey) {
		t.Helper()
		got, err := items.CheckMaterialAvailability(f.ctx, keys)
		if err != nil || !slices.Equal(got, want) {
			t.Fatal("availability mismatch:", got, want, err)
		}
	}
	check([]domain.MaterialVersionKey{
		b, {MaterialID: first.ID, RevisionID: working.ID}, a,
		{MaterialID: draft.ID, RevisionID: draft.CurrentRevisionID}, {MaterialID: first.ID, RevisionID: second.PublishedRevisionID},
	}, []domain.MaterialVersionKey{b, a})

	next := domain.MaterialVersionKey{MaterialID: first.ID, RevisionID: publishWithoutBaseAttachments(t, f, first)}
	check([]domain.MaterialVersionKey{a, next}, []domain.MaterialVersionKey{next})
	_, err = f.store.SetMaterialModeration(f.ctx, domain.SetMaterialModerationInput{
		ActorID: uuid.NewString(), Reason: "fixture reason",
		MaterialModeration: domain.MaterialModeration{MaterialID: next.MaterialID, PublishedRevisionID: next.RevisionID, Hidden: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	check([]domain.MaterialVersionKey{next, b}, []domain.MaterialVersionKey{b})
	f.state(t, second, domain.ContentStatusArchived)
	check([]domain.MaterialVersionKey{next, b}, nil)
}

func BenchmarkMaterialAvailability(b *testing.B) {
	f := newActivityFixture(b)
	keys := make([]domain.MaterialVersionKey, 0, domain.MaxMaterialAvailabilityBatch)
	for range domain.MaxMaterialAvailabilityBatch {
		item := f.state(b, f.create(b), domain.ContentStatusPublished)
		keys = append(keys, domain.MaterialVersionKey{MaterialID: item.ID, RevisionID: item.PublishedRevisionID})
	}
	b.Run("individual", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for i := range keys {
				got, err := f.store.AvailableMaterialVersions(f.ctx, keys[i:i+1])
				if err != nil || len(got) != 1 {
					b.Fatal("single availability lookup failed:", err)
				}
			}
		}
	})
	b.Run("batch", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			got, err := f.store.AvailableMaterialVersions(f.ctx, keys)
			if err != nil || len(got) != len(keys) {
				b.Fatal("batch availability lookup failed:", err)
			}
		}
	})
}
