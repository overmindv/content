package domain

import "time"

type ReviewThread struct {
	ID         int64
	RevisionID string
	LineNumber int32
	AuthorID   string
	Body       string
	CreatedAt  time.Time
	ResolvedAt *time.Time
	ResolvedBy string
}

type ReviewReply struct {
	ID        int64
	AuthorID  string
	Body      string
	CreatedAt time.Time
}

type ReviewActor struct {
	ProposalID string
	ActorID    string
}

type CreateReviewThreadInput struct {
	ReviewActor
	RevisionID string
	LineNumber int32
	Body       string
}

type ReviewThreadRef struct {
	ReviewActor
	ThreadID int64
}

type ReviewPageInput struct {
	ReviewActor
	AfterID  int64
	PageSize int32
}

type ReviewReplyPageInput struct {
	ReviewPageInput
	ThreadID int64
}

type ReviewPage[T any] struct {
	Items       []T
	NextAfterID int64
}

type ReviewDecisionKind string

const (
	ReviewApprove        ReviewDecisionKind = "approve"
	ReviewRequestChanges ReviewDecisionKind = "request_changes"
)

type ReviewRevisionRef struct {
	ReviewActor
	RevisionID string
}

type SubmitReviewDecisionInput struct {
	ReviewRevisionRef
	Decision ReviewDecisionKind
	Reason   string
}

type ReviewDecision struct {
	ReviewerID string
	Decision   ReviewDecisionKind
	Reason     string
	CreatedAt  time.Time
}

type ReviewRevisionStatus struct {
	RevisionID string
	Current    bool
	Decisions  []ReviewDecision
}
