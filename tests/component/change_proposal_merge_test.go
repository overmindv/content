//go:build component

package component

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/tests/builders"
)

func TestMergeProposalConflictAndRebase(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := postgres.NewContentItemStore(pool, postgres.WithOutbox(true, "content-merge-test"))
	input := builders.ContentItem()
	owner, topic, peer, asset := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	input.CreatedBy, input.Slug = owner, input.Slug+"-"+uuid.NewString()
	input.Learning = &domain.LearningMetadata{TopicIDs: []string{topic}}
	input.Assets = []domain.CreateContentAssetInput{{AssetID: asset, Kind: domain.AssetKindAttachment, Title: "Reference"}}
	item, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := store.Delete(ctx, item.ID, owner); err != nil {
			t.Error(err)
		}
	}()
	base := item.CurrentRevisionID
	if _, err = store.ChangeState(ctx, domain.ChangeContentItemStateInput{
		ID: item.ID, ExpectedRevisionID: base, ActorID: owner,
	}, domain.ContentStatusPublished); err != nil {
		t.Fatal(err)
	}
	draft, err := store.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID: item.ID, ExpectedRevisionID: base, CreatedBy: owner,
		Format: domain.ContentFormatMarkdown, Source: "# Private draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	open := func(source string) domain.ChangeProposal {
		t.Helper()
		proposal, err := store.OpenChangeProposal(ctx, domain.OpenChangeProposalInput{
			ContentItemID: item.ID, BaseRevisionID: base, AuthorID: uuid.NewString(), Source: source,
		})
		if err != nil {
			t.Fatal(err)
		}
		return proposal
	}
	assign := func(proposal domain.ChangeProposal) {
		t.Helper()
		if err := store.AssignReviewers(ctx, domain.ReviewAssignmentRequest{
			ProposalID: proposal.ID, RevisionID: proposal.Head.ID, TopicID: topic, RankedCandidateIDs: []string{owner, peer},
		}); err != nil {
			t.Fatal(err)
		}
	}
	vote := func(proposal domain.ChangeProposal, reviewer string) {
		t.Helper()
		if _, err := store.SubmitReviewDecision(ctx, domain.SubmitReviewDecisionInput{
			ReviewRevisionRef: domain.ReviewRevisionRef{
				ReviewActor: domain.ReviewActor{ProposalID: proposal.ID, ActorID: reviewer}, RevisionID: proposal.Head.ID,
			},
			Decision: domain.ReviewApprove,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mergeInput := func(proposal domain.ChangeProposal) domain.MergeChangeProposalInput {
		return domain.MergeChangeProposalInput{ProposalID: proposal.ID, ExpectedHeadID: proposal.Head.ID, ActorID: owner}
	}
	assertPublication := func(published string, revisionCount, eventCount int) {
		t.Helper()
		view, err := store.Get(ctx, item.ID, owner)
		if err != nil || view.PublishedRevisionID != published || view.CurrentRevisionID != draft.ID {
			t.Fatalf("owner view: %+v, %v", view, err)
		}
		revisions, err := store.ListRevisions(ctx, item.ID, owner)
		if err != nil || len(revisions) != revisionCount {
			t.Errorf("revision count: got %d, want %d: %v", len(revisions), revisionCount, err)
		}
		if n := pendingOutboxRows(t, ctx, pool, item.ID); n != eventCount {
			t.Errorf("outbox count: got %d, want %d", n, eventCount)
		}
	}
	first, second := open("# First proposal"), open("# Second proposal")
	for _, proposal := range []domain.ChangeProposal{first, second} {
		assign(proposal)
		vote(proposal, owner)
		if _, err = store.MergeChangeProposal(ctx, mergeInput(proposal)); !errors.Is(err, service.ErrReviewRequired) {
			t.Fatalf("merged with one approval: %v", err)
		}
		vote(proposal, peer)
	}
	foreign := mergeInput(first)
	foreign.ActorID = first.AuthorID
	if _, err = store.MergeChangeProposal(ctx, foreign); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("proposal author published another user's material: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE content_change_proposal_revisions SET review_verified_until = now() WHERE proposal_id = $1 AND id = $2
	`, first.ID, first.Head.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.MergeChangeProposal(ctx, mergeInput(first)); !errors.Is(err, service.ErrReviewRequired) {
		t.Errorf("expired reviewer eligibility accepted: %v", err)
	}
	assign(first)
	brokenOutbox := postgres.NewContentItemStore(pool, postgres.WithOutbox(true, "test\x00"))
	if _, err = brokenOutbox.MergeChangeProposal(ctx, mergeInput(first)); err == nil || !strings.Contains(err.Error(), "insert content item event") {
		t.Fatalf("expected outbox failure: %v", err)
	}
	assertPublication(base, 2, 3)
	if proposal, err := store.GetChangeProposal(ctx, first.ID, owner); err != nil || proposal.Status != domain.ChangeProposalOpen {
		t.Fatalf("merge status did not roll back: %+v, %v", proposal, err)
	}

	type outcome struct {
		proposal domain.ChangeProposal
		merge    domain.MergeChangeProposalResult
		err      error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for _, proposal := range []domain.ChangeProposal{first, second} {
		go func() {
			<-start
			result, err := store.MergeChangeProposal(ctx, mergeInput(proposal))
			results <- outcome{proposal, result, err}
		}()
	}
	close(start)
	winner, conflict := <-results, <-results
	if winner.err != nil {
		winner, conflict = conflict, winner
	}
	publishedID := winner.merge.PublishedRevisionID
	if winner.err != nil || publishedID == "" || !errors.Is(conflict.err, service.ErrMergeConflict) {
		t.Fatalf("concurrent merges: winner %v, conflict %v", winner.err, conflict.err)
	}
	assertPublication(publishedID, 3, 4)
	merged, err := store.GetChangeProposal(ctx, winner.proposal.ID, winner.proposal.AuthorID)
	if err != nil || merged.Status != domain.ChangeProposalMerged || merged.ClosedAt == nil || merged.MergedRevisionID != publishedID {
		t.Errorf("merged proposal missing publication: %+v, %v", merged, err)
	}
	reader, err := store.Get(ctx, item.ID, peer)
	if err != nil || reader.CurrentRevision == nil || reader.CurrentRevision.Source != winner.proposal.Head.Source ||
		reader.CurrentRevision.CreatedBy != winner.proposal.AuthorID || len(reader.Assets) != 1 || reader.Assets[0].AssetID != asset {
		t.Errorf("publication snapshot: %+v, %v", reader, err)
	}
	var event struct {
		Event string             `json:"event"`
		Item  domain.ContentItem `json:"item"`
	}
	var payload []byte
	if err = pool.QueryRow(ctx, `SELECT message_value FROM outbox WHERE message_key = $1 ORDER BY created_at DESC LIMIT 1`, item.ID).
		Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(payload, &event); err != nil || event.Event != "content_item.updated" ||
		event.Item.CurrentRevisionID != publishedID || event.Item.CurrentRevision == nil || event.Item.CurrentRevision.Source != winner.proposal.Head.Source {
		t.Errorf("outbox contains the wrong version: %v", err)
	}

	stale := conflict.proposal
	actor := domain.ReviewActor{ProposalID: stale.ID, ActorID: owner}
	thread, err := store.CreateReviewThread(ctx, domain.CreateReviewThreadInput{
		ReviewActor: actor, RevisionID: stale.Head.ID, Body: "Check the example",
	})
	if err != nil {
		t.Fatal(err)
	}
	staleState, err := store.GetChangeProposal(ctx, stale.ID, stale.AuthorID)
	if err != nil || !staleState.BaseOutdated || staleState.Status != domain.ChangeProposalOpen {
		t.Fatalf("conflict missing from proposal: %+v, %v", staleState, err)
	}
	change := domain.RebaseChangeProposalInput{
		AppendChangeProposalRevisionInput: domain.AppendChangeProposalRevisionInput{
			ProposalID: stale.ID, ExpectedHeadID: stale.Head.ID, AuthorID: stale.AuthorID, Source: "# Updated base",
		},
		BaseRevisionID: base,
	}
	if _, err = store.RebaseChangeProposal(ctx, change); !errors.Is(err, service.ErrMergeConflict) {
		t.Errorf("accepted an obsolete rebase target: %v", err)
	}
	change.BaseRevisionID, change.AuthorID = publishedID, owner
	if _, err = store.RebaseChangeProposal(ctx, change); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("material owner rebased someone else's proposal: %v", err)
	}
	change.AuthorID = stale.AuthorID
	rebased, err := store.RebaseChangeProposal(ctx, change)
	if err != nil || rebased.BaseRevisionID != publishedID || rebased.BaseOutdated || rebased.Head.ParentRevisionID != stale.Head.ID {
		t.Fatalf("rebase result: %+v, %v", rebased, err)
	}
	oldDiff, err := store.GetChangeProposalDiffSources(ctx, domain.ChangeProposalDiffInput{
		ProposalID: stale.ID, RevisionID: stale.Head.ID, ActorID: stale.AuthorID,
	})
	if err != nil || oldDiff.BaseRevisionID != base {
		t.Errorf("historical diff changed base: %+v, %v", oldDiff, err)
	}
	history, err := store.ListChangeProposalHistory(ctx, domain.ChangeProposalHistoryInput{
		ProposalID: stale.ID, ActorID: stale.AuthorID, PageSize: 10,
	})
	if err != nil || len(history.Entries) != 2 || history.Entries[0].BaseRevisionID != publishedID || history.Entries[1].BaseRevisionID != base {
		t.Errorf("history did not preserve both bases: %+v, %v", history, err)
	}
	next, err := store.AppendChangeProposalRevision(ctx, domain.AppendChangeProposalRevisionInput{
		ProposalID: rebased.ID, ExpectedHeadID: rebased.Head.ID, AuthorID: rebased.AuthorID, Source: "# Final version",
	})
	if err != nil || next.BaseRevisionID != publishedID {
		t.Fatalf("append lost the rebased version: %+v, %v", next, err)
	}
	if _, err = store.MergeChangeProposal(ctx, mergeInput(stale)); !errors.Is(err, service.ErrRevisionConflict) {
		t.Errorf("stale head merged: %v", err)
	}
	ref := domain.ReviewRevisionRef{ReviewActor: actor, RevisionID: next.Head.ID}
	status, err := store.GetReviewRevisionStatus(ctx, ref)
	if err != nil || !status.HasUnresolvedDiscussions {
		t.Errorf("open discussion on an older revision was not reported: %v", err)
	}
	if outcome, _ := status.Outcome(); outcome == domain.ReviewApproved {
		t.Error("previous approvals carried into rebase")
	}
	if _, err = store.MergeChangeProposal(ctx, mergeInput(next)); !errors.Is(err, service.ErrUnresolvedDiscussions) {
		t.Errorf("merge with unresolved discussion: %v", err)
	}
	assign(next)
	vote(next, owner)
	vote(next, peer)
	if _, err = store.MergeChangeProposal(ctx, mergeInput(next)); !errors.Is(err, service.ErrUnresolvedDiscussions) {
		t.Errorf("quorum bypassed the discussion: %v", err)
	}
	assertPublication(publishedID, 3, 4)
	threadRef := domain.ReviewThreadRef{ReviewActor: actor, ThreadID: thread.ID}
	if _, err = store.SetReviewThreadResolved(ctx, threadRef, true); err != nil {
		t.Fatal(err)
	}
	status, err = store.GetReviewRevisionStatus(ctx, ref)
	if err != nil || status.HasUnresolvedDiscussions {
		t.Errorf("resolved discussion still reported as open: %v", err)
	}
	last, err := store.MergeChangeProposal(ctx, mergeInput(next))
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := store.MergeChangeProposal(ctx, mergeInput(winner.proposal))
	if err != nil || repeated.PublishedRevisionID != publishedID {
		t.Errorf("repeat merge did not return its original publication: %+v, %v", repeated, err)
	}
	assertPublication(last.PublishedRevisionID, 4, 5)
	if _, err = store.SetReviewThreadResolved(ctx, threadRef, false); !errors.Is(err, service.ErrInvalidState) {
		t.Errorf("discussion was reopened after merge: %v", err)
	}
}
