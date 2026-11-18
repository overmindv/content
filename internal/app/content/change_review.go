package content

import (
	"context"

	"github.com/overmindv/content/internal/app/content/mapper"
	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
)

func (s *Server) CreateReviewThread(ctx context.Context, req *contentpb.CreateReviewThreadRequest) (*contentpb.CreateReviewThreadResponse, error) {
	actor, err := reviewActor(ctx, req.GetProposalId())
	if err != nil {
		return nil, err
	}
	thread, err := s.review.CreateThread(ctx, domain.CreateReviewThreadInput{
		ReviewActor: actor, RevisionID: req.GetRevisionId(), LineNumber: req.GetLineNumber(), Body: req.GetBody(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	return &contentpb.CreateReviewThreadResponse{Thread: mapper.ToProtoReviewThread(thread)}, nil
}

func (s *Server) ReplyToReviewThread(ctx context.Context, req *contentpb.ReplyToReviewThreadRequest) (*contentpb.ReplyToReviewThreadResponse, error) {
	actor, err := reviewActor(ctx, req.GetProposalId())
	if err != nil {
		return nil, err
	}
	reply, err := s.review.Reply(ctx, domain.ReviewThreadRef{ReviewActor: actor, ThreadID: req.GetThreadId()}, req.GetBody())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &contentpb.ReplyToReviewThreadResponse{Reply: mapper.ToProtoReviewReply(reply)}, nil
}

func (s *Server) SetReviewThreadResolved(ctx context.Context, req *contentpb.SetReviewThreadResolvedRequest) (*contentpb.SetReviewThreadResolvedResponse, error) {
	actor, err := reviewActor(ctx, req.GetProposalId())
	if err != nil {
		return nil, err
	}
	thread, err := s.review.Resolve(ctx, domain.ReviewThreadRef{ReviewActor: actor, ThreadID: req.GetThreadId()}, req.GetResolved())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &contentpb.SetReviewThreadResolvedResponse{Thread: mapper.ToProtoReviewThread(thread)}, nil
}

func (s *Server) ListReviewThreads(ctx context.Context, req *contentpb.ListReviewThreadsRequest) (*contentpb.ListReviewThreadsResponse, error) {
	actor, err := reviewActor(ctx, req.GetProposalId())
	if err != nil {
		return nil, err
	}
	page, err := s.review.ListThreads(ctx, domain.ReviewPageInput{ReviewActor: actor, AfterID: req.GetAfterId(), PageSize: req.GetPageSize()})
	if err != nil {
		return nil, toStatusError(err)
	}
	threads := make([]*contentpb.ReviewThread, 0, len(page.Items))
	for _, thread := range page.Items {
		threads = append(threads, mapper.ToProtoReviewThread(thread))
	}
	return &contentpb.ListReviewThreadsResponse{Threads: threads, NextAfterId: page.NextAfterID}, nil
}

func (s *Server) ListReviewReplies(ctx context.Context, req *contentpb.ListReviewRepliesRequest) (*contentpb.ListReviewRepliesResponse, error) {
	actor, err := reviewActor(ctx, req.GetProposalId())
	if err != nil {
		return nil, err
	}
	page, err := s.review.ListReplies(ctx, domain.ReviewReplyPageInput{
		ReviewPageInput: domain.ReviewPageInput{ReviewActor: actor, AfterID: req.GetAfterId(), PageSize: req.GetPageSize()},
		ThreadID:        req.GetThreadId(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	replies := make([]*contentpb.ReviewReply, 0, len(page.Items))
	for _, reply := range page.Items {
		replies = append(replies, mapper.ToProtoReviewReply(reply))
	}
	return &contentpb.ListReviewRepliesResponse{Replies: replies, NextAfterId: page.NextAfterID}, nil
}

func (s *Server) SubmitReviewDecision(ctx context.Context, req *contentpb.SubmitReviewDecisionRequest) (*contentpb.SubmitReviewDecisionResponse, error) {
	actor, err := reviewActor(ctx, req.GetProposalId())
	if err != nil {
		return nil, err
	}
	decision, err := s.review.Submit(ctx, domain.SubmitReviewDecisionInput{
		ReviewRevisionRef: domain.ReviewRevisionRef{ReviewActor: actor, RevisionID: req.GetRevisionId()},
		Decision:          domain.ReviewDecisionKind(req.GetDecision()), Reason: req.GetReason(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	return &contentpb.SubmitReviewDecisionResponse{Decision: mapper.ToProtoReviewDecision(decision)}, nil
}

func (s *Server) GetReviewRevisionStatus(ctx context.Context, req *contentpb.GetReviewRevisionStatusRequest) (*contentpb.GetReviewRevisionStatusResponse, error) {
	actor, err := reviewActor(ctx, req.GetProposalId())
	if err != nil {
		return nil, err
	}
	status, err := s.review.Status(ctx, domain.ReviewRevisionRef{ReviewActor: actor, RevisionID: req.GetRevisionId()})
	if err != nil {
		return nil, toStatusError(err)
	}
	decisions := make([]*contentpb.ReviewDecision, 0, len(status.Decisions))
	for _, d := range status.Decisions {
		decisions = append(decisions, mapper.ToProtoReviewDecision(d))
	}
	assignments := make([]*contentpb.ReviewAssignment, 0, len(status.Assignments))
	for _, a := range status.Assignments {
		assignments = append(assignments, &contentpb.ReviewAssignment{ReviewerId: a.ReviewerID, Slot: int32(a.Slot)})
	}
	outcome, wait := status.Outcome()
	return &contentpb.GetReviewRevisionStatusResponse{
		RevisionId: status.RevisionID, Current: status.Current, Decisions: decisions, Assignments: assignments,
		Outcome: string(outcome), WaitReason: string(wait), TopicId: status.TopicID,
	}, nil
}

func reviewActor(ctx context.Context, id string) (domain.ReviewActor, error) {
	actor, err := actorID(ctx)
	return domain.ReviewActor{ProposalID: id, ActorID: actor}, err
}
