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

func TestChangeReviewPermissionsAndHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	store := postgres.NewContentItemStore(pool)
	input := builders.ContentItem()
	input.CreatedBy = uuid.NewString()
	input.Slug += "-" + uuid.NewString()
	item, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	owner, author, outsider := input.CreatedBy, uuid.NewString(), uuid.NewString()
	defer func() {
		if _, err := store.Delete(ctx, item.ID, owner); err != nil {
			t.Error(err)
		}
	}()
	state := domain.ChangeContentItemStateInput{ID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, ActorID: owner}
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusPublished); err != nil {
		t.Fatal(err)
	}
	open := domain.OpenChangeProposalInput{ContentItemID: item.ID, BaseRevisionID: item.CurrentRevisionID, AuthorID: author, Source: "# New\nSecond line"}
	proposal, err := store.OpenChangeProposal(ctx, open)
	if err != nil {
		t.Fatal(err)
	}
	open.AuthorID = owner
	selfProposal, err := store.OpenChangeProposal(ctx, open)
	if err != nil {
		t.Fatal(err)
	}
	actor := domain.ReviewActor{ProposalID: proposal.ID, ActorID: author}
	root := domain.CreateReviewThreadInput{ReviewActor: actor, RevisionID: proposal.Head.ID, LineNumber: 2, Body: "Check this line"}
	thread, err := store.CreateReviewThread(ctx, root)
	if err != nil || thread.AuthorID != author || thread.LineNumber != 2 || thread.RevisionID != proposal.Head.ID {
		t.Fatalf("thread was not anchored to the second line: %v", err)
	}
	root.LineNumber = 3
	if _, err = store.CreateReviewThread(ctx, root); !validator.IsValidationError(err) {
		t.Errorf("out of bounds line: %v", err)
	}
	root.LineNumber, root.RevisionID = 0, selfProposal.Head.ID
	if _, err = store.CreateReviewThread(ctx, root); !errors.Is(err, service.ErrProposalRevisionNotFound) {
		t.Errorf("thread anchored to another proposal: %v", err)
	}
	root.RevisionID = proposal.Head.ID
	if _, err = store.CreateReviewThread(ctx, root); err != nil {
		t.Fatal(err)
	}
	root.ActorID = outsider
	if _, err = store.CreateReviewThread(ctx, root); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("outsider created a thread: %v", err)
	}
	ref := domain.ReviewThreadRef{ReviewActor: actor, ThreadID: thread.ID}
	ref.ActorID = owner
	for _, body := range []string{"First reply", "Second reply"} {
		if _, err = store.ReplyToReviewThread(ctx, ref, body); err != nil {
			t.Fatal(err)
		}
	}
	wrongThread := ref
	wrongThread.ProposalID = selfProposal.ID
	if _, err = store.ReplyToReviewThread(ctx, wrongThread, "Wrong proposal"); !errors.Is(err, service.ErrReviewThreadNotFound) {
		t.Errorf("reply to a thread in another proposal: %v", err)
	}
	pageInput := domain.ReviewPageInput{ReviewActor: actor, PageSize: 1}
	page, err := store.ListReviewThreads(ctx, pageInput)
	if err != nil || len(page.Items) != 1 || page.NextAfterID != thread.ID {
		t.Fatalf("first thread page: %+v, %v", page, err)
	}
	pageInput.AfterID = page.NextAfterID
	page, err = store.ListReviewThreads(ctx, pageInput)
	if err != nil || len(page.Items) != 1 || page.NextAfterID != 0 || page.Items[0].LineNumber != 0 {
		t.Errorf("second thread page: %+v, %v", page, err)
	}
	replyInput := domain.ReviewReplyPageInput{ReviewPageInput: domain.ReviewPageInput{ReviewActor: actor, PageSize: 1}, ThreadID: thread.ID}
	replies, err := store.ListReviewReplies(ctx, replyInput)
	if err != nil || len(replies.Items) != 1 || replies.NextAfterID == 0 || replies.Items[0].Body != "First reply" {
		t.Fatalf("first reply page: %+v, %v", replies, err)
	}
	replyInput.AfterID = replies.NextAfterID
	replies, err = store.ListReviewReplies(ctx, replyInput)
	if err != nil || len(replies.Items) != 1 || replies.NextAfterID != 0 || replies.Items[0].Body != "Second reply" {
		t.Errorf("second reply page: %+v, %v", replies, err)
	}
	replyInput.ActorID = outsider
	if _, err = store.ListReviewReplies(ctx, replyInput); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("outsider read replies: %v", err)
	}
	ref.ActorID = outsider
	if _, err = store.SetReviewThreadResolved(ctx, ref, true); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("outsider resolved the thread: %v", err)
	}
	ref.ActorID = author
	resolved, err := store.SetReviewThreadResolved(ctx, ref, true)
	if err != nil || resolved.ResolvedAt == nil || resolved.ResolvedBy != author {
		t.Fatalf("resolve: %+v, %v", resolved, err)
	}
	resolved, err = store.SetReviewThreadResolved(ctx, ref, false)
	if err != nil || resolved.ResolvedAt != nil || resolved.ResolvedBy != "" {
		t.Errorf("reopen: %+v, %v", resolved, err)
	}

	decision := domain.SubmitReviewDecisionInput{
		ReviewRevisionRef: domain.ReviewRevisionRef{ReviewActor: domain.ReviewActor{ProposalID: selfProposal.ID, ActorID: owner}, RevisionID: selfProposal.Head.ID},
		Decision:          domain.ReviewApprove,
	}
	if _, err = store.SubmitReviewDecision(ctx, decision); !errors.Is(err, service.ErrReviewNotAllowed) {
		t.Errorf("material owner approved their own proposal: %v", err)
	}
	decision.ProposalID, decision.RevisionID, decision.ActorID = proposal.ID, proposal.Head.ID, outsider
	if _, err = store.SubmitReviewDecision(ctx, decision); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("outsider decided on proposal: %v", err)
	}
	decision.ActorID = owner
	approved, err := store.SubmitReviewDecision(ctx, decision)
	if err != nil || approved.ReviewerID != owner || approved.Decision != domain.ReviewApprove {
		t.Fatalf("approve: %+v, %v", approved, err)
	}
	if replay, err := store.SubmitReviewDecision(ctx, decision); err != nil || !replay.CreatedAt.Equal(approved.CreatedAt) {
		t.Errorf("repeat approval: %+v, %v", replay, err)
	}
	decision.Decision, decision.Reason = domain.ReviewRequestChanges, "Need another example"
	if _, err = store.SubmitReviewDecision(ctx, decision); !errors.Is(err, service.ErrReviewDecisionExists) {
		t.Errorf("different decision overwrote approval: %v", err)
	}
	updated, err := store.AppendChangeProposalRevision(ctx, domain.AppendChangeProposalRevisionInput{
		ProposalID: proposal.ID, ExpectedHeadID: proposal.Head.ID, AuthorID: author, Source: "# Newer revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SubmitReviewDecision(ctx, decision); !errors.Is(err, service.ErrRevisionConflict) {
		t.Errorf("decision on old revision: %v", err)
	}
	current, err := store.GetReviewRevisionStatus(ctx, domain.ReviewRevisionRef{ReviewActor: actor})
	if err != nil || !current.Current || current.RevisionID != updated.Head.ID || len(current.Decisions) != 0 {
		t.Errorf("approval carried over to a new revision: %+v, %v", current, err)
	}
	historic, err := store.GetReviewRevisionStatus(ctx, domain.ReviewRevisionRef{ReviewActor: actor, RevisionID: proposal.Head.ID})
	if err != nil || historic.Current || len(historic.Decisions) != 1 || historic.Decisions[0].Decision != domain.ReviewApprove {
		t.Errorf("historic approval missing or still current: %+v, %v", historic, err)
	}
	decision.RevisionID = updated.Head.ID
	if _, err = store.SubmitReviewDecision(ctx, decision); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CloseChangeProposal(ctx, proposal.ID, author); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReplyToReviewThread(ctx, ref, "After close"); !errors.Is(err, service.ErrInvalidState) {
		t.Errorf("reply on closed proposal: %v", err)
	}
	if page, err = store.ListReviewThreads(ctx, domain.ReviewPageInput{ReviewActor: actor, PageSize: 5}); err != nil || len(page.Items) != 2 || page.Items[0].RevisionID != proposal.Head.ID {
		t.Errorf("threads disappeared or lost their anchors: %+v, %v", page, err)
	}

	t.Run("approval races with a new revision", func(t *testing.T) {
		open.AuthorID = author
		racing, err := store.OpenChangeProposal(ctx, open)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		approval := make(chan error, 1)
		appendErr := make(chan error, 1)
		go func() {
			<-start
			_, err := store.SubmitReviewDecision(ctx, domain.SubmitReviewDecisionInput{
				ReviewRevisionRef: domain.ReviewRevisionRef{ReviewActor: domain.ReviewActor{ProposalID: racing.ID, ActorID: owner}, RevisionID: racing.Head.ID},
				Decision:          domain.ReviewApprove,
			})
			approval <- err
		}()
		go func() {
			<-start
			_, err := store.AppendChangeProposalRevision(ctx, domain.AppendChangeProposalRevisionInput{
				ProposalID: racing.ID, ExpectedHeadID: racing.Head.ID, AuthorID: author, Source: "# Edit during review",
			})
			appendErr <- err
		}()
		close(start)
		if err = <-appendErr; err != nil {
			t.Error(err)
		}
		if err = <-approval; err != nil && !errors.Is(err, service.ErrRevisionConflict) {
			t.Errorf("concurrent review: %v", err)
		}
		status, err := store.GetReviewRevisionStatus(ctx, domain.ReviewRevisionRef{
			ReviewActor: domain.ReviewActor{ProposalID: racing.ID, ActorID: owner},
		})
		if err != nil || status.RevisionID == racing.Head.ID || len(status.Decisions) != 0 {
			t.Errorf("concurrent approval became active on the new revision: %+v, %v", status, err)
		}
	})
}
