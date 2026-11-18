//go:build component

package component

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/internal/pkg/validator"
	"github.com/overmindv/content/tests/builders"
)

func TestReviewAssignmentsAndQuorum(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("COMPONENT_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := postgres.NewContentItemStore(pool)
	input := builders.ContentItem()
	owner, author, second, third, replacement, topic := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	input.CreatedBy = owner
	input.Learning = &domain.LearningMetadata{TopicIDs: []string{topic, uuid.NewString()}}
	input.Slug += "-" + uuid.NewString()
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
	open := domain.OpenChangeProposalInput{ContentItemID: item.ID, BaseRevisionID: item.CurrentRevisionID, AuthorID: author, Source: "# Changes"}
	proposal, err := store.OpenChangeProposal(ctx, open)
	if err != nil {
		t.Fatal(err)
	}
	refFor := func(actor string) domain.ReviewRevisionRef {
		return domain.ReviewRevisionRef{
			ReviewActor: domain.ReviewActor{ProposalID: proposal.ID, ActorID: actor}, RevisionID: proposal.Head.ID,
		}
	}
	checkStatus := func(outcome domain.ReviewOutcome, reason domain.ReviewWaitReason, reviewers ...string) domain.ReviewRevisionStatus {
		t.Helper()
		status, err := store.GetReviewRevisionStatus(ctx, refFor(author))
		if got, why := status.Outcome(); err != nil || got != outcome || why != reason || len(status.Assignments) != len(reviewers) {
			t.Fatalf("status=%+v (%s, %s), err=%v", status, got, why, err)
		}
		for i, id := range reviewers {
			if status.Assignments[i].ReviewerID != id {
				t.Fatalf("reviewers=%+v, want %v", status.Assignments, reviewers)
			}
		}
		return status
	}
	vote := func(actor string, kind domain.ReviewDecisionKind) {
		t.Helper()
		if _, err := store.SubmitReviewDecision(ctx, domain.SubmitReviewDecisionInput{
			ReviewRevisionRef: refFor(actor), Decision: kind, Reason: "Reviewed",
		}); err != nil {
			t.Fatal(err)
		}
	}
	assign := domain.ReviewAssignmentRequest{ProposalID: proposal.ID, RevisionID: proposal.Head.ID, TopicID: topic}
	setCandidates := func(ids ...string) {
		t.Helper()
		assign.RankedCandidateIDs = ids
		if err := store.AssignReviewers(ctx, assign); err != nil {
			t.Fatal(err)
		}
	}
	checkStatus(domain.ReviewWaitingCandidates, domain.ReviewNeedsInitialReviewers)
	wrong := assign
	wrong.TopicID = uuid.NewString()
	if err = store.AssignReviewers(ctx, wrong); !validator.IsValidationError(err) {
		t.Errorf("unrelated topic: %v", err)
	}
	wrong = assign
	wrong.RankedCandidateIDs = make([]string, 65)
	if err = store.AssignReviewers(ctx, wrong); !validator.IsValidationError(err) {
		t.Errorf("unbounded candidate list: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO content_change_review_decisions (proposal_id, revision_id, reviewer_id, decision, reason)
		VALUES ($1, $2, $3, 'approve', '')
	`, proposal.ID, proposal.Head.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	setCandidates(author, owner)
	status := checkStatus(domain.ReviewWaitingCandidates, domain.ReviewNeedsInitialReviewers, owner)
	if status.TopicID != topic || len(status.Decisions) != 1 || !status.Decisions[0].Legacy {
		t.Errorf("unassigned vote migrated into quorum: %+v", status)
	}
	wrong = assign
	wrong.TopicID = input.Learning.TopicIDs[1]
	if err = store.AssignReviewers(ctx, wrong); !validator.IsValidationError(err) {
		t.Errorf("changed topic in an active cycle: %v", err)
	}
	setCandidates(author, second, second, third, owner)
	checkStatus(domain.ReviewPending, "", owner, second)
	diff := domain.ChangeProposalDiffInput{ProposalID: proposal.ID, ActorID: second, RevisionID: proposal.Head.ID}
	if _, err = store.GetChangeProposalDiffSources(ctx, diff); err != nil {
		t.Errorf("assigned reviewer cannot read diff: %v", err)
	}
	thread, err := store.CreateReviewThread(ctx, domain.CreateReviewThreadInput{
		ReviewActor: refFor(second).ReviewActor, RevisionID: proposal.Head.ID, Body: "Please clarify this",
	})
	if err != nil {
		t.Fatal(err)
	}
	page := domain.ReviewPageInput{ReviewActor: refFor(second).ReviewActor, PageSize: 20}
	if result, err := store.ListReviewThreads(ctx, page); err != nil || len(result.Items) != 1 || result.Items[0].ID != thread.ID {
		t.Errorf("assigned reviewer cannot read discussion: %+v, %v", result, err)
	}
	vote(owner, domain.ReviewApprove)
	vote(second, domain.ReviewRequestChanges)
	checkStatus(domain.ReviewWaitingCandidates, domain.ReviewNeedsTiebreaker, owner, second)
	setCandidates(author, owner, second)
	checkStatus(domain.ReviewWaitingCandidates, domain.ReviewNeedsTiebreaker, owner, second)
	setCandidates(author, second, third, replacement)
	checkStatus(domain.ReviewPending, "", owner, second, third)
	vote(third, domain.ReviewApprove)
	checkStatus(domain.ReviewApproved, "", owner, second, third)

	if err = store.RevokeReviewAssignment(ctx, proposal.ID, proposal.Head.ID, second); err != nil {
		t.Fatal(err)
	}
	checkStatus(domain.ReviewWaitingCandidates, domain.ReviewNeedsInitialReviewers, owner)
	if _, err = store.GetChangeProposalDiffSources(ctx, diff); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("revoked reviewer can read diff: %v", err)
	}
	if _, err = store.GetChangeProposal(ctx, proposal.ID, third); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("tiebreak reviewer can still read proposal after revocation: %v", err)
	}
	if _, err = store.ListReviewThreads(ctx, page); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("revoked reviewer can read discussion: %v", err)
	}
	if _, err = store.SubmitReviewDecision(ctx, domain.SubmitReviewDecisionInput{
		ReviewRevisionRef: refFor(second), Decision: domain.ReviewApprove,
	}); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("revoked reviewer can vote: %v", err)
	}
	var keptVotes int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM content_change_review_assignments
		WHERE proposal_id = $1 AND revision_id = $2 AND revoked_at IS NOT NULL AND decided_at IS NOT NULL
	`, proposal.ID, proposal.Head.ID).Scan(&keptVotes)
	if err != nil || keptVotes != 2 {
		t.Errorf("votes lost after revocation: %d, %v", keptVotes, err)
	}
	setCandidates(second, third, replacement)
	vote(replacement, domain.ReviewApprove)
	checkStatus(domain.ReviewApproved, "", owner, replacement)

	open.AuthorID = owner
	proposal, err = store.OpenChangeProposal(ctx, open)
	if err != nil {
		t.Fatal(err)
	}
	author = owner
	assign.ProposalID, assign.RevisionID = proposal.ID, proposal.Head.ID
	setCandidates(owner, second, second, third, replacement)
	checkStatus(domain.ReviewPending, "", second, third)
	vote(second, domain.ReviewRequestChanges)
	vote(third, domain.ReviewRequestChanges)
	checkStatus(domain.ReviewChangesRequested, "", second, third)
}
