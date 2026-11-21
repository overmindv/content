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
	"github.com/overmindv/content/internal/pkg/validator"
	"github.com/overmindv/content/tests/builders"
)

func gallerySource(ids ...string) string {
	return "# Before\n\n```ovm-photos\n" + strings.Join(ids, "\n") + "\n```\n\nAfter."
}

func TestInlinePhotoDraftSnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := postgres.NewContentItemStore(pool)
	input := builders.ContentItem()
	input.CreatedBy, input.Slug = uuid.NewString(), uuid.NewString()
	foreign, photo := uuid.NewString(), uuid.NewString()
	input.Assets = []domain.CreateContentAssetInput{{AssetID: foreign, Kind: domain.AssetKindImage}}
	if _, err = store.Create(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.Slug = uuid.NewString()
	input.Source = gallerySource(foreign)
	input.Assets[0] = domain.CreateContentAssetInput{AssetID: photo, Kind: domain.AssetKindImage}
	if _, err = store.Create(ctx, input); !validator.IsValidationError(err) {
		t.Error("accepted a photo from another material", err)
	}
	input.Source = gallerySource(photo)
	item, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal("cannot save a photo from the current snapshot", err)
	}
	draft := domain.CreateContentRevisionInput{
		ContentItemID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, CreatedBy: input.CreatedBy,
		Format: input.Format, Source: input.Source, Attachments: &domain.AttachmentSet{},
	}
	if _, err = store.CreateRevision(ctx, draft); !validator.IsValidationError(err) {
		t.Error("removed an image still referenced by the text", err)
	}
	draft.Attachments.Assets = []domain.CreateContentAssetInput{{AssetID: photo, Kind: domain.AssetKindPDF}}
	if _, err = store.CreateRevision(ctx, draft); !validator.IsValidationError(err) {
		t.Error("accepted a PDF as an inline photo", err)
	}
	draft.Attachments = nil
	draft.Source += "\nMore text."
	revision, err := store.CreateRevision(ctx, draft)
	if err != nil {
		t.Fatal("cannot save a gallery that inherits attachments", err)
	}
	state := domain.ChangeContentItemStateInput{ID: item.ID, ExpectedRevisionID: revision.ID, ActorID: input.CreatedBy}
	if _, err = pool.Exec(ctx, `UPDATE content_revisions SET source = $2 WHERE id = $1`, revision.ID, gallerySource(foreign)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusPublished); !validator.IsValidationError(err) {
		t.Error("published a revision whose gallery references another material", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE content_revisions SET source = $2 WHERE id = $1`, revision.ID, draft.Source); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusPublished); err != nil {
		t.Fatal(err)
	}
	published, err := store.Get(ctx, item.ID, "")
	if err != nil || published.CurrentRevision == nil || published.CurrentRevision.Source != draft.Source ||
		len(published.Assets) != 1 || published.Assets[0].AssetID != photo {
		t.Error("published text and photo do not share the revision", err)
	}
}

//nolint:paralleltest // The fixture claims from the shared Media binding queue.
func TestInlinePhotosAfterReview(t *testing.T) {
	f := newMergeFixture(t, postgres.WithManagedAttachments())
	ctx := t.Context()
	author, first, second := uuid.NewString(), uuid.NewString(), uuid.NewString()
	media, _, _ := mediaBindingFixture(t, ctx, map[string]string{first: author, second: author}, "")
	source := gallerySource(second, first)
	proposal := f.proposal(author, source, &domain.AttachmentSet{Assets: []domain.CreateContentAssetInput{
		{AssetID: first, Kind: domain.AssetKindImage}, {AssetID: second, Kind: domain.AssetKindImage, Position: 1},
	}})
	if _, err := f.store.AppendChangeProposalRevision(ctx, domain.AppendChangeProposalRevisionInput{
		ProposalID: proposal.ID, ExpectedHeadID: proposal.Head.ID, AuthorID: author,
		Source: source + "\nEdit.", Attachments: &domain.AttachmentSet{},
	}); !validator.IsValidationError(err) {
		t.Error("accepted a proposal revision without its referenced photos", err)
	}
	worker := service.NewMediaBindingWorker(f.store, media)
	deliver := func() {
		t.Helper()
		if found, err := worker.DeliverNext(ctx); !found || err != nil {
			t.Fatal("cannot apply attachment bindings", found, err)
		}
	}
	deliver()
	f.approve(proposal)
	if _, err := f.merge(proposal); !errors.Is(err, service.ErrAttachmentsPending) {
		t.Fatal("merge must wait for copied attachment bindings", err)
	}
	deliver()
	merged, err := f.merge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	f.checkPublication(merged.PublishedRevisionID)
	published, err := f.store.Get(ctx, f.item.ID, "")
	if err != nil || published.CurrentRevision == nil || published.CurrentRevision.Source != source || len(published.Assets) != 2 {
		t.Fatal("gallery was lost during merge", err)
	}
	for _, asset := range published.Assets {
		if asset.RevisionID != merged.PublishedRevisionID || asset.Kind != domain.AssetKindImage {
			t.Error("published gallery used the wrong attachment snapshot")
		}
	}
}
