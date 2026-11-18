package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

type ChangeReviewService struct {
	store ChangeReviewStore
}

func NewChangeReviewService(store ChangeReviewStore) *ChangeReviewService {
	return &ChangeReviewService{store: store}
}

func (s *ChangeReviewService) CreateThread(ctx context.Context, input domain.CreateReviewThreadInput) (domain.ReviewThread, error) {
	if !validProposalID(input.ProposalID) || !validProposalID(input.RevisionID) || input.LineNumber < 0 {
		return domain.ReviewThread{}, validator.Error{Message: "invalid proposal, revision or line number"}
	}
	body, err := reviewText(input.Body, true)
	if err != nil {
		return domain.ReviewThread{}, err
	}
	input.Body = body
	return s.store.CreateReviewThread(ctx, input)
}

func (s *ChangeReviewService) Reply(ctx context.Context, ref domain.ReviewThreadRef, body string) (domain.ReviewReply, error) {
	if err := validateReviewThreadRef(ref); err != nil {
		return domain.ReviewReply{}, err
	}
	text, err := reviewText(body, true)
	if err != nil {
		return domain.ReviewReply{}, err
	}
	return s.store.ReplyToReviewThread(ctx, ref, text)
}

func (s *ChangeReviewService) Resolve(ctx context.Context, ref domain.ReviewThreadRef, resolved bool) (domain.ReviewThread, error) {
	if err := validateReviewThreadRef(ref); err != nil {
		return domain.ReviewThread{}, err
	}
	return s.store.SetReviewThreadResolved(ctx, ref, resolved)
}

func (s *ChangeReviewService) ListThreads(ctx context.Context, input domain.ReviewPageInput) (domain.ReviewPage[domain.ReviewThread], error) {
	if err := normalizeReviewPage(&input); err != nil {
		return domain.ReviewPage[domain.ReviewThread]{}, err
	}
	return s.store.ListReviewThreads(ctx, input)
}

func (s *ChangeReviewService) ListReplies(ctx context.Context, input domain.ReviewReplyPageInput) (domain.ReviewPage[domain.ReviewReply], error) {
	if input.ThreadID <= 0 {
		return domain.ReviewPage[domain.ReviewReply]{}, validator.Error{Message: "invalid thread_id"}
	}
	if err := normalizeReviewPage(&input.ReviewPageInput); err != nil {
		return domain.ReviewPage[domain.ReviewReply]{}, err
	}
	return s.store.ListReviewReplies(ctx, input)
}

func (s *ChangeReviewService) Submit(ctx context.Context, input domain.SubmitReviewDecisionInput) (domain.ReviewDecision, error) {
	if !validProposalID(input.ProposalID) || !validProposalID(input.RevisionID) {
		return domain.ReviewDecision{}, validator.Error{Message: "proposal_id and revision_id must be UUIDs"}
	}
	if input.Decision != domain.ReviewApprove && input.Decision != domain.ReviewRequestChanges {
		return domain.ReviewDecision{}, validator.Error{Message: "decision must be approve or request_changes"}
	}
	reason, err := reviewText(input.Reason, input.Decision == domain.ReviewRequestChanges)
	if err != nil {
		return domain.ReviewDecision{}, err
	}
	input.RevisionID = strings.ToLower(input.RevisionID)
	input.Reason = reason
	return s.store.SubmitReviewDecision(ctx, input)
}

func (s *ChangeReviewService) Status(ctx context.Context, ref domain.ReviewRevisionRef) (domain.ReviewRevisionStatus, error) {
	if !validProposalID(ref.ProposalID) || ref.RevisionID != "" && !validProposalID(ref.RevisionID) {
		return domain.ReviewRevisionStatus{}, validator.Error{Message: "invalid proposal_id or revision_id"}
	}
	ref.RevisionID = strings.ToLower(ref.RevisionID)
	return s.store.GetReviewRevisionStatus(ctx, ref)
}

func validateReviewThreadRef(ref domain.ReviewThreadRef) error {
	if !validProposalID(ref.ProposalID) || ref.ThreadID <= 0 {
		return validator.Error{Message: "invalid proposal_id or thread_id"}
	}
	return nil
}

func normalizeReviewPage(input *domain.ReviewPageInput) error {
	if !validProposalID(input.ProposalID) || input.AfterID < 0 || input.PageSize < 0 || input.PageSize > 50 {
		return validator.Error{Message: "invalid proposal or page (maximum page size 50)"}
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	return nil
}

func reviewText(s string, required bool) (string, error) {
	if len(s) > 4096 || !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 {
		return "", validator.Error{Message: "review text must be UTF-8 without NUL, up to 4096 bytes"}
	}
	s = strings.TrimSpace(s)
	if required && s == "" {
		return "", validator.Error{Message: "review text is required"}
	}
	return s, nil
}
