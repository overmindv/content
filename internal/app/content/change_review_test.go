package content

import (
	"testing"

	"github.com/overmindv/content/internal/mock"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCChangeReview(t *testing.T) {
	t.Parallel()

	store := mock.NewMockChangeReviewStore(gomock.NewController(t))
	ctx, client := newGRPCTestClient(t, testServiceToken, NewServer(nil, nil, service.NewChangeReviewService(store)))
	thread := &contentpb.CreateReviewThreadRequest{ProposalId: testOtherActor, RevisionId: testOtherActor, LineNumber: 2, Body: " Comment "}
	if _, err := client.CreateReviewThread(ctx, thread); status.Code(err) != codes.Unauthenticated {
		t.Errorf("create thread without credentials: %v", err)
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	actor := domain.ReviewActor{ProposalID: testOtherActor, ActorID: testActor}
	store.EXPECT().CreateReviewThread(gomock.Any(), domain.CreateReviewThreadInput{
		ReviewActor: actor, RevisionID: testOtherActor, LineNumber: 2, Body: "Comment",
	}).Return(domain.ReviewThread{ID: 5, AuthorID: testActor, LineNumber: 2}, nil)
	ref := domain.ReviewRevisionRef{ReviewActor: actor, RevisionID: testOtherActor}
	store.EXPECT().SubmitReviewDecision(gomock.Any(), domain.SubmitReviewDecisionInput{
		ReviewRevisionRef: ref, Decision: domain.ReviewApprove,
	}).Return(domain.ReviewDecision{}, service.ErrReviewNotAllowed)
	store.EXPECT().GetReviewRevisionStatus(gomock.Any(), ref).
		Return(domain.ReviewRevisionStatus{
			RevisionID: testOtherActor, TopicID: testOtherActor,
			Assignments: []domain.ReviewAssignment{{Slot: 1, ReviewerID: testActor, Decision: domain.ReviewApprove}},
			Decisions:   []domain.ReviewDecision{{ReviewerID: testActor, Decision: domain.ReviewApprove}},
		}, nil)

	response, err := client.CreateReviewThread(ctx, thread)
	if err != nil || response.GetThread().GetId() != 5 || response.GetThread().GetAuthorId() != testActor || response.GetThread().GetResolved() {
		t.Errorf("create thread: %+v, %v", response, err)
	}
	decision := &contentpb.SubmitReviewDecisionRequest{ProposalId: testOtherActor, RevisionId: testOtherActor, Decision: "approve"}
	if _, err = client.SubmitReviewDecision(ctx, decision); status.Code(err) != codes.PermissionDenied {
		t.Errorf("self-approval: %v", err)
	}
	decision.Decision, decision.Reason = "request_changes", "   "
	if _, err = client.SubmitReviewDecision(ctx, decision); status.Code(err) != codes.InvalidArgument {
		t.Errorf("request changes with no reason: %v", err)
	}
	if _, err = client.ListReviewThreads(ctx, &contentpb.ListReviewThreadsRequest{ProposalId: testOtherActor, PageSize: 51}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("list with oversized page: %v", err)
	}
	review, err := client.GetReviewRevisionStatus(ctx, &contentpb.GetReviewRevisionStatusRequest{ProposalId: testOtherActor, RevisionId: testOtherActor})
	if err != nil || review.GetCurrent() || len(review.GetDecisions()) != 1 {
		t.Errorf("historical review: %+v, %v", review, err)
	}
	if review.GetOutcome() != "waiting_candidates" || review.GetWaitReason() != "initial_reviewers" ||
		review.GetTopicId() != testOtherActor || len(review.GetAssignments()) != 1 || review.GetAssignments()[0].GetReviewerId() != testActor {
		t.Errorf("review assignments: %+v", review)
	}
}
