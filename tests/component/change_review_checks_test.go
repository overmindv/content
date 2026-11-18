//go:build component

package component

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestReviewCheckWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("COMPONENT_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := postgres.NewContentItemStore(pool)
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
		if _, err := store.Delete(ctx, item.ID, owner); err != nil {
			t.Error(err)
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
		var input struct {
			TopicID         string   `json:"topic_id"`
			AssigneeIDs     []string `json:"assignee_ids"`
			MaterialOwnerID string   `json:"material_owner_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.TopicID != topic || input.MaterialOwnerID != owner {
			t.Error("unexpected candidate lookup")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		ranked, eligible := []string{peer, third, owner}, make([]string, 0, len(input.AssigneeIDs))
		if revoked.Load() {
			ranked = []string{third, owner}
		}
		for _, id := range input.AssigneeIDs {
			if id != peer || !revoked.Load() {
				eligible = append(eligible, id)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"topic_id": topic, "ranked_candidate_ids": ranked, "eligible_assignee_ids": eligible})
	}))
	defer source.Close()
	client, err := external.NewReviewCandidateClient(source.URL, "test-service")
	if err != nil {
		t.Fatal(err)
	}
	worker := service.NewReviewCheckWorker(store, client)
	check := func() {
		t.Helper()
		if claimed, err := worker.CheckNext(ctx); !claimed || err != nil {
			t.Fatalf("check claimed=%t: %v", claimed, err)
		}
	}
	vote := func(reviewer string, decision domain.ReviewDecisionKind) error {
		_, err := store.SubmitReviewDecision(ctx, domain.SubmitReviewDecisionInput{
			ReviewRevisionRef: domain.ReviewRevisionRef{
				ReviewActor: domain.ReviewActor{ProposalID: proposal.ID, ActorID: reviewer}, RevisionID: proposal.Head.ID,
			},
			Decision: decision, Reason: "Reviewed",
		})
		return err
	}
	stateOf := func(want domain.ReviewOutcome, reason domain.ReviewWaitReason, slots int) {
		t.Helper()
		ref := domain.ReviewRevisionRef{ReviewActor: domain.ReviewActor{ProposalID: proposal.ID, ActorID: author}}
		status, err := store.GetReviewRevisionStatus(ctx, ref)
		if got, why := status.Outcome(); err != nil || got != want || why != reason || len(status.Assignments) != slots {
			t.Fatalf("review=%+v (%s, %s), err=%v", status, got, why, err)
		}
	}
	makeDue := func(expire bool) {
		t.Helper()
		_, err := pool.Exec(ctx, `
			UPDATE content_change_proposal_revisions
			SET review_due_at = NOW(), review_verified_until = CASE WHEN $3::boolean THEN NOW() ELSE review_verified_until END
			WHERE proposal_id = $1 AND id = $2
		`, proposal.ID, proposal.Head.ID, expire)
		if err != nil {
			t.Fatal(err)
		}
	}

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
	if found, err := worker.CheckNext(ctx); !found || err == nil {
		t.Errorf("candidate source failure was not reported: %v %v", found, err)
	}
	stateOf(domain.ReviewWaitingCandidates, domain.ReviewEligibilityUnverified, 3)
	if _, err = store.GetChangeProposal(ctx, proposal.ID, peer); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("expired reviewer has access: %v", err)
	}
	if err = vote(owner, domain.ReviewApprove); !errors.Is(err, service.ErrReviewNotAllowed) {
		t.Errorf("expired eligibility accepted a vote: %v", err)
	}
	unavailable.Store(false)
	makeDue(false)
	check()
	stateOf(domain.ReviewApproved, "", 3)
	makeDue(false)
	stale, found, err := store.ClaimReviewCheck(ctx)
	if err != nil || !found {
		t.Fatalf("claim review before revocation: %v", err)
	}
	if _, found, err := store.ClaimReviewCheck(ctx); err != nil || found {
		t.Errorf("another worker claimed the same check: %v %v", found, err)
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
}
