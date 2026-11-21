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
	Legacy     bool
}

type ReviewRevisionStatus struct {
	RevisionID  string
	Current     bool
	TopicID     string
	Assignments []ReviewAssignment
	Decisions   []ReviewDecision
}

type ReviewAssignment struct {
	ReviewerID string
	Slot       int16
	Decision   ReviewDecisionKind
}

type ReviewAssignmentRequest struct {
	ProposalID         string
	RevisionID         string
	TopicID            string
	RankedCandidateIDs []string
}

type ReviewOutcome string

const (
	ReviewWaitingCandidates ReviewOutcome = "waiting_candidates"
	ReviewPending           ReviewOutcome = "pending"
	ReviewApproved          ReviewOutcome = "approved"
	ReviewChangesRequested  ReviewOutcome = "changes_requested"
)

type ReviewWaitReason string

const (
	ReviewNeedsInitialReviewers ReviewWaitReason = "initial_reviewers"
	ReviewNeedsTiebreaker       ReviewWaitReason = "tiebreak_reviewer"
)

func (s ReviewRevisionStatus) Outcome() (ReviewOutcome, ReviewWaitReason) {
	var slots [3]*ReviewAssignment
	for i := range s.Assignments {
		a := &s.Assignments[i]
		if a.Slot >= 1 && a.Slot <= 3 {
			slots[a.Slot-1] = a
		}
	}
	first, second, third := slots[0], slots[1], slots[2]
	if first == nil || second == nil {
		return ReviewWaitingCandidates, ReviewNeedsInitialReviewers
	}
	if first.Decision == "" || second.Decision == "" {
		return ReviewPending, ""
	}
	majority := first.Decision
	if first.Decision != second.Decision {
		if third == nil {
			return ReviewWaitingCandidates, ReviewNeedsTiebreaker
		}
		if third.Decision == "" {
			return ReviewPending, ""
		}
		majority = third.Decision
	}
	if majority == ReviewApprove {
		return ReviewApproved, ""
	}

	return ReviewChangesRequested, ""
}
