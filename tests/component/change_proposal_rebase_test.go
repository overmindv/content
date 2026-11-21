//go:build component

package component

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/tests/builders"
)

func TestRebaseChangeProposal(t *testing.T) {
	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := postgres.NewContentItemStore(pool, postgres.WithManagedAttachments())
	created := builders.ContentItem()
	created.CreatedBy, created.Slug = uuid.NewString(), uuid.NewString()
	item, err := store.Create(ctx, created)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if _, cleanupErr := pool.Exec(cleanupCtx, `DELETE FROM content_media_bindings WHERE material_id = $1`, item.ID); cleanupErr != nil {
			t.Error("could not clean up fixture bindings", cleanupErr)
		}
	})
	publish := domain.ChangeContentItemStateInput{ID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, ActorID: created.CreatedBy}
	if _, publishErr := store.ChangeState(ctx, publish, domain.ContentStatusPublished); publishErr != nil {
		t.Fatal(publishErr)
	}
	base := item.CurrentRevisionID
	author, attachment := uuid.NewString(), uuid.NewString()
	proposal, err := store.OpenChangeProposal(ctx, domain.OpenChangeProposalInput{
		ContentItemID: item.ID, BaseRevisionID: base, AuthorID: author, Source: "# Suggested wording",
		Attachments: &domain.AttachmentSet{Assets: []domain.CreateContentAssetInput{{AssetID: attachment, Kind: "image"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	const newSource = "# New published basis"
	revisionInput := domain.CreateContentRevisionInput{
		ContentItemID: item.ID, ExpectedRevisionID: base, Source: newSource,
		Format: domain.ContentFormatMarkdown, CreatedBy: created.CreatedBy,
	}
	revision, err := store.CreateRevision(ctx, revisionInput)
	if err != nil {
		t.Fatal(err)
	}
	rebase := domain.RebaseChangeProposalInput{
		AppendChangeProposalRevisionInput: domain.AppendChangeProposalRevisionInput{
			ProposalID: proposal.ID, ExpectedHeadID: proposal.Head.ID, AuthorID: author, Source: "# Reworked suggestion",
		},
		BaseRevisionID: revision.ID,
	}
	if _, rebaseErr := store.RebaseChangeProposal(ctx, rebase); !errors.Is(rebaseErr, service.ErrMergeConflict) {
		t.Fatal("unpublished revision accepted as the basis", rebaseErr)
	}
	publish.ExpectedRevisionID = revision.ID
	if _, publishErr := store.ChangeState(ctx, publish, domain.ContentStatusPublished); publishErr != nil {
		t.Fatal(publishErr)
	}
	asOwner := rebase
	asOwner.AuthorID = created.CreatedBy
	if _, rebaseErr := store.RebaseChangeProposal(ctx, asOwner); !errors.Is(rebaseErr, service.ErrProposalNotFound) {
		t.Fatal("only the proposal author can rebase", rebaseErr)
	}
	outdated := rebase
	outdated.BaseRevisionID = base
	if _, rebaseErr := store.RebaseChangeProposal(ctx, outdated); !errors.Is(rebaseErr, service.ErrMergeConflict) {
		t.Fatal("old publication accepted as the basis", rebaseErr)
	}
	rebased := raceProposalOperation(t, service.ErrRevisionConflict, func(_ int) (domain.ChangeProposal, error) {
		return store.RebaseChangeProposal(ctx, rebase)
	})
	if rebased.BaseOutdated || rebased.BaseRevisionID != revision.ID || rebased.Head.Ordinal != 2 ||
		rebased.Head.ParentRevisionID != proposal.Head.ID || len(rebased.Head.Assets) != 1 || rebased.Head.Assets[0].AssetID != attachment {
		t.Fatal("rebase lost the new basis, history or attachment")
	}
	var parent string
	bindingErr := pool.QueryRow(ctx, `SELECT inherit_from_revision_id FROM content_media_bindings WHERE revision_id = $1`,
		rebased.Head.ID).Scan(&parent)
	if bindingErr != nil || parent != proposal.Head.ID {
		t.Fatal("inherited attachment binding was not queued", bindingErr)
	}
	edit := rebase.AppendChangeProposalRevisionInput
	edit.ExpectedHeadID, edit.Source = rebased.Head.ID, "# One more edit"
	appended, err := store.AppendChangeProposalRevision(ctx, edit)
	if err != nil || appended.BaseRevisionID != revision.ID || appended.BaseOutdated {
		t.Fatal("next edit did not retain the rebased basis", err)
	}
	entryIDs := []string{appended.Head.ID, rebased.Head.ID, proposal.Head.ID}
	bases := []string{revision.ID, revision.ID, base}
	sources := []string{newSource, newSource, created.Source}
	proposalHistory := domain.ChangeProposalHistoryInput{ProposalID: proposal.ID, ActorID: author, PageSize: 1}
	for i, id := range entryIDs {
		page, pageErr := store.ListChangeProposalHistory(ctx, proposalHistory)
		if pageErr != nil || len(page.Entries) != 1 || page.Entries[0].ID != id || page.Entries[0].BaseRevisionID != bases[i] {
			t.Fatal("history lost a revision basis", pageErr)
		}
		proposalHistory.BeforeOrdinal = page.NextBeforeOrdinal
		diff, diffErr := store.GetChangeProposalDiffSources(ctx, domain.ChangeProposalDiffInput{
			ProposalID: proposal.ID, ActorID: author, RevisionID: id,
		})
		if diffErr != nil || diff.BaseRevisionID != bases[i] || diff.BaseSource != sources[i] {
			t.Fatal("diff used the wrong historical basis", diffErr)
		}
	}
	if _, deleteErr := store.Delete(ctx, item.ID, created.CreatedBy); deleteErr != nil {
		t.Fatal(deleteErr)
	}
}
