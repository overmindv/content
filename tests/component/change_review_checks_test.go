//go:build component

package component

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/service/external"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/tests/builders"
)

//nolint:paralleltest // The worker claims from the component database queue.
func TestReviewCheckWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := postgres.NewContentItemStore(pool, postgres.WithReviewCheckScheduling())
	owner, author, peer, third, topic := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	input := builders.ContentItem()
	input.CreatedBy = owner
	input.Slug += "-" + uuid.NewString()
	input.Learning = &domain.LearningMetadata{TopicIDs: []string{topic}}
	item, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, cleanupErr := store.Delete(ctx, item.ID, owner); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	}()
	state := domain.ChangeContentItemStateInput{ID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, ActorID: owner}
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusPublished); err != nil {
		t.Fatal(err)
	}
	proposal, err := store.OpenChangeProposal(ctx, domain.OpenChangeProposalInput{
		ContentItemID: item.ID, BaseRevisionID: item.CurrentRevisionID, AuthorID: author, Source: "# Changes",
	})
	if err != nil {
		t.Fatal(err)
	}

	var revoked, unavailable atomic.Bool
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var lookup struct {
			TopicID         string   `json:"topic_id"`
			AssigneeIDs     []string `json:"assignee_ids"`
			MaterialOwnerID string   `json:"material_owner_id"`
		}
		if decodeErr := json.NewDecoder(r.Body).Decode(&lookup); decodeErr != nil || lookup.TopicID != topic || lookup.MaterialOwnerID != owner {
			t.Error("unexpected candidate lookup")
			w.WriteHeader(http.StatusBadRequest)

			return
		}
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		ranked, eligible := []string{peer, third, owner}, make([]string, 0, len(lookup.AssigneeIDs))
		if revoked.Load() {
			ranked = []string{third, owner}
		}
		for _, id := range lookup.AssigneeIDs {
			if id != peer || !revoked.Load() {
				eligible = append(eligible, id)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"topic_id": topic, "ranked_candidate_ids": ranked, "eligible_assignee_ids": eligible})
	}))
	defer source.Close()
	client, err := external.NewReviewCandidateClient(source.URL, "fixture-review-candidates-service")
	if err != nil {
		t.Fatal(err)
	}
	worker := service.NewReviewCheckWorker(store, client)
	check := func() {
		t.Helper()
		if claimed, checkErr := worker.CheckNext(ctx); !claimed || checkErr != nil {
			t.Fatal("check claimed:", claimed, "error:", checkErr)
		}
	}
	vote := func(reviewer string, decision domain.ReviewDecisionKind) error {
		_, voteErr := store.SubmitReviewDecision(ctx, domain.SubmitReviewDecisionInput{
			ReviewRevisionRef: domain.ReviewRevisionRef{
				ReviewActor: domain.ReviewActor{ProposalID: proposal.ID, ActorID: reviewer}, RevisionID: proposal.Head.ID,
			},
			Decision: decision, Reason: "Reviewed",
		})

		return voteErr
	}
	stateOf := func(want domain.ReviewOutcome, reason domain.ReviewWaitReason, slots int) {
		t.Helper()
		ref := domain.ReviewRevisionRef{ReviewActor: domain.ReviewActor{ProposalID: proposal.ID, ActorID: author}}
		status, statusErr := store.GetReviewRevisionStatus(ctx, ref)
		if got, why := status.Outcome(); statusErr != nil || got != want || why != reason || len(status.Assignments) != slots {
			t.Fatal("review:", status, "outcome:", got, "reason:", why, "error:", statusErr)
		}
	}
	makeDue := func(expire bool) {
		t.Helper()
		_, updateErr := pool.Exec(ctx, `
			UPDATE content_change_proposal_revisions
			SET review_due_at = NOW(), review_verified_until = CASE WHEN $3::boolean THEN NOW() ELSE review_verified_until END
			WHERE proposal_id = $1 AND id = $2
		`, proposal.ID, proposal.Head.ID, expire)
		if updateErr != nil {
			t.Fatal(updateErr)
		}
	}

	const setHidden = `UPDATE content_items SET moderation_hidden = $2 WHERE id = $1`
	if _, err = pool.Exec(ctx, setHidden, item.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, claimed, claimErr := store.ClaimReviewCheck(ctx); claimed || claimErr != nil {
		t.Fatal("hidden material was sent to the candidate source:", claimed, claimErr)
	}
	var scheduled bool
	if err = pool.QueryRow(ctx, `
		SELECT review_due_at IS NOT NULL FROM content_change_proposal_revisions
		WHERE proposal_id = $1 AND id = $2`, proposal.ID, proposal.Head.ID).Scan(&scheduled); err != nil || !scheduled {
		t.Fatal("temporarily hidden material lost its next review check:", err)
	}
	if _, err = pool.Exec(ctx, setHidden, item.ID, false); err != nil {
		t.Fatal(err)
	}
	makeDue(false)
	check()
	stateOf(domain.ReviewPending, "", 2)
	if err = vote(owner, domain.ReviewApprove); err != nil {
		t.Fatal(err)
	}
	if err = vote(peer, domain.ReviewRequestChanges); err != nil {
		t.Fatal(err)
	}
	stateOf(domain.ReviewWaitingCandidates, domain.ReviewNeedsTiebreaker, 2)
	check()
	stateOf(domain.ReviewPending, "", 3)
	if err = vote(third, domain.ReviewApprove); err != nil {
		t.Fatal(err)
	}
	stateOf(domain.ReviewApproved, "", 3)
	unavailable.Store(true)
	makeDue(true)
	if found, checkErr := worker.CheckNext(ctx); !found || checkErr == nil {
		t.Errorf("candidate source failure was not reported: %v %v", found, checkErr)
	}
	stateOf(domain.ReviewWaitingCandidates, domain.ReviewEligibilityUnverified, 3)
	if _, err = store.GetChangeProposal(ctx, proposal.ID, peer); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("expired reviewer has access: %v", err)
	}
	if err = vote(owner, domain.ReviewApprove); !errors.Is(err, service.ErrReviewNotAllowed) {
		t.Errorf("expired eligibility accepted a vote: %v", err)
	}
	merge := domain.MergeChangeProposalInput{ProposalID: proposal.ID, ExpectedHeadID: proposal.Head.ID, ActorID: owner}
	if _, mergeErr := store.MergeChangeProposal(ctx, merge); !errors.Is(mergeErr, service.ErrReviewRequired) {
		t.Error("expired approval did not block publication", mergeErr)
	}
	ref := domain.ReviewActor{ProposalID: proposal.ID, ActorID: peer}
	if _, threadErr := store.CreateReviewThread(ctx, domain.CreateReviewThreadInput{ReviewActor: ref,
		RevisionID: proposal.Head.ID, Body: "Expired reviewer"}); !errors.Is(threadErr, service.ErrProposalNotFound) {
		t.Error("expired reviewer could write a thread", threadErr)
	}
	if _, diffErr := store.GetChangeProposalDiffSources(ctx, domain.ChangeProposalDiffInput{ProposalID: proposal.ID,
		ActorID: peer}); !errors.Is(diffErr, service.ErrProposalNotFound) {
		t.Error("expired reviewer could read the diff", diffErr)
	}
	unavailable.Store(false)
	makeDue(false)
	check()
	stateOf(domain.ReviewApproved, "", 3)
	makeDue(false)
	stale, found, err := store.ClaimReviewCheck(ctx)
	if err != nil || !found {
		t.Fatal("claim review before revocation:", err)
	}
	if _, claimed, checkErr := store.ClaimReviewCheck(ctx); checkErr != nil || claimed {
		t.Errorf("another worker claimed the same check: %v %v", claimed, checkErr)
	}
	revoked.Store(true)
	makeDue(false)
	check()
	if err = store.ApplyReviewCheck(ctx, stale, domain.ReviewCandidates{
		RankedIDs: []string{peer, third, owner}, EligibleIDs: []string{peer, third, owner},
	}); err != nil {
		t.Fatal(err)
	}
	stateOf(domain.ReviewWaitingCandidates, domain.ReviewNeedsInitialReviewers, 1)
	if _, err = store.GetChangeProposal(ctx, proposal.ID, peer); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("revoked reviewer has access: %v", err)
	}

	newProposal, err := store.AppendChangeProposalRevision(ctx, domain.AppendChangeProposalRevisionInput{
		ProposalID: proposal.ID, ExpectedHeadID: proposal.Head.ID, AuthorID: author, Source: "# New revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	proposal = newProposal
	check()
	stateOf(domain.ReviewPending, "", 2)
	if _, err = store.CloseChangeProposal(ctx, proposal.ID, author); err != nil {
		t.Fatal(err)
	}
	legacy, err := postgres.NewContentItemStore(pool).OpenChangeProposal(ctx, domain.OpenChangeProposalInput{
		ContentItemID: item.ID, BaseRevisionID: item.CurrentRevisionID, AuthorID: author, Source: "# Older open proposal",
	})
	if err != nil {
		t.Fatal(err)
	}
	isScheduled := func(p domain.ChangeProposal) bool {
		t.Helper()
		var scheduled bool
		if scanErr := pool.QueryRow(ctx, `
			SELECT review_due_at IS NOT NULL FROM content_change_proposal_revisions WHERE proposal_id = $1 AND id = $2
		`, p.ID, p.Head.ID).Scan(&scheduled); scanErr != nil {
			t.Fatal(scanErr)
		}

		return scheduled
	}
	if isScheduled(legacy) {
		t.Error("the disabled worker queued a new proposal")
	}
	if err = store.ScheduleOpenReviewChecks(ctx); err != nil {
		t.Fatal(err)
	}
	if !isScheduled(legacy) || isScheduled(proposal) {
		t.Error("startup did not distinguish open and closed proposals")
	}
}
