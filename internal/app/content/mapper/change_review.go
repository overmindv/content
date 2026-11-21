package mapper

import (
	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ToProtoReviewThread(thread domain.ReviewThread) *contentpb.ReviewThread {
	return &contentpb.ReviewThread{
		Id: thread.ID, RevisionId: thread.RevisionID, LineNumber: thread.LineNumber,
		AuthorId: thread.AuthorID, Body: thread.Body, CreatedAt: timestamppb.New(thread.CreatedAt),
		Resolved: thread.ResolvedAt != nil, ResolvedAt: timestampOrNil(thread.ResolvedAt), ResolvedBy: thread.ResolvedBy,
	}
}

func ToProtoReviewReply(reply domain.ReviewReply) *contentpb.ReviewReply {
	return &contentpb.ReviewReply{Id: reply.ID, AuthorId: reply.AuthorID, Body: reply.Body, CreatedAt: timestamppb.New(reply.CreatedAt)}
}

func ToProtoReviewDecision(decision domain.ReviewDecision) *contentpb.ReviewDecision {
	return &contentpb.ReviewDecision{
		ReviewerId: decision.ReviewerID, Decision: string(decision.Decision), Reason: decision.Reason,
		CreatedAt: timestamppb.New(decision.CreatedAt), Legacy: decision.Legacy,
	}
}
