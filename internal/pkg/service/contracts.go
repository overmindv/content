package service

import (
	"context"
	"errors"

	"github.com/overmindv/content/internal/pkg/domain"
)

// Публикация событий контента атомарно выполняется хранилищем через transaction
// outbox (в той же транзакции, что и бизнес-изменение), поэтому сервис не держит
// собственного publisher-контракта.

var (
	ErrNotFound                  = errors.New("content item not found")
	ErrRevisionConflict          = errors.New("current revision changed")
	ErrMergeConflict             = errors.New("MERGE_CONFLICT")
	ErrReviewRequired            = errors.New("REVIEW_REQUIRED")
	ErrUnresolvedReviewThreads   = errors.New("UNRESOLVED_REVIEW_THREADS")
	ErrInvalidState              = errors.New("operation is not allowed in this content state")
	ErrProposalNotFound          = errors.New("change proposal not found")
	ErrProposalExists            = errors.New("an open proposal already exists for this material and author")
	ErrProposalNoChange          = errors.New("proposal must change the text or attachments")
	ErrProposalRevisionNotFound  = errors.New("change proposal revision not found")
	ErrProposalDiffBusy          = errors.New("too many proposal diff requests")
	ErrReviewThreadNotFound      = errors.New("review thread not found")
	ErrReviewNotAllowed          = errors.New("only an assigned reviewer can submit a decision")
	ErrReviewDecisionExists      = errors.New("a different decision already exists for this revision")
	ErrInactiveTopic             = errors.New("topic does not exist or is not active")
	ErrTopicCatalogUnavailable   = errors.New("topic catalog is unavailable")
	ErrMediaUnavailable          = errors.New("media is unavailable")
	ErrAttachmentNotReady        = errors.New("attachment is not available for binding")
	ErrAttachmentsPending        = errors.New("revision attachments are not bound yet")
	ErrPublishedRevisionNotFound = errors.New("published source revision not found")
)

type ContentItemStore interface {
	PingContext(ctx context.Context) error
	Create(ctx context.Context, input domain.CreateContentItemInput) (domain.ContentItem, error)
	Get(ctx context.Context, id, actorID string) (domain.ContentItem, error)
	CheckPublishedAttachment(context.Context, domain.PublishedAttachmentRef) error
	SnapshotMaterials(context.Context, domain.MaterialSnapshotInput) (domain.MaterialSnapshot, error)
	ConfirmMaterialRead(context.Context, domain.ConfirmMaterialReadInput) error
	List(ctx context.Context, actorID string) ([]domain.ContentItem, error)
	ListCatalog(context.Context, domain.CatalogFilter, *domain.CatalogCursor) ([]domain.CatalogEntry, error)
	Update(ctx context.Context, input domain.UpdateContentItemInput) (domain.ContentItem, error)
	ChangeState(ctx context.Context, input domain.ChangeContentItemStateInput, target domain.ContentStatus) (domain.ContentItem, error)
	RestorePublishedRevision(context.Context, domain.RestorePublishedRevisionInput) (domain.ContentRevision, error)
	Delete(ctx context.Context, id, actorID string) (domain.ContentItem, error)
	CreateRevision(ctx context.Context, input domain.CreateContentRevisionInput) (domain.ContentRevision, error)
	ListRevisions(ctx context.Context, contentItemID, actorID string) ([]domain.ContentRevision, error)
	ListRevisionHistory(context.Context, domain.RevisionHistoryInput) ([]domain.RevisionSummary, error)
}

type TopicChecker interface {
	CheckActiveTopic(ctx context.Context, topicID string) error
}

type ChangeProposalStore interface {
	OpenChangeProposal(context.Context, domain.OpenChangeProposalInput) (domain.ChangeProposal, error)
	GetChangeProposal(ctx context.Context, id, actorID string) (domain.ChangeProposal, error)
	AppendChangeProposalRevision(context.Context, domain.AppendChangeProposalRevisionInput) (domain.ChangeProposal, error)
	RebaseChangeProposal(context.Context, domain.RebaseChangeProposalInput) (domain.ChangeProposal, error)
	MergeChangeProposal(context.Context, domain.MergeChangeProposalInput) (domain.MergeChangeProposalResult, error)
	CloseChangeProposal(ctx context.Context, id, actorID string) (domain.ChangeProposal, error)
	ListChangeProposalHistory(context.Context, domain.ChangeProposalHistoryInput) (domain.ChangeProposalHistory, error)
	GetChangeProposalDiffSources(context.Context, domain.ChangeProposalDiffInput) (domain.ChangeProposalDiffSources, error)
}

type ChangeReviewStore interface {
	CreateReviewThread(context.Context, domain.CreateReviewThreadInput) (domain.ReviewThread, error)
	ReplyToReviewThread(context.Context, domain.ReviewThreadRef, string) (domain.ReviewReply, error)
	SetReviewThreadResolved(context.Context, domain.ReviewThreadRef, bool) (domain.ReviewThread, error)
	ListReviewThreads(context.Context, domain.ReviewPageInput) (domain.ReviewPage[domain.ReviewThread], error)
	ListReviewReplies(context.Context, domain.ReviewReplyPageInput) (domain.ReviewPage[domain.ReviewReply], error)
	SubmitReviewDecision(context.Context, domain.SubmitReviewDecisionInput) (domain.ReviewDecision, error)
	GetReviewRevisionStatus(context.Context, domain.ReviewRevisionRef) (domain.ReviewRevisionStatus, error)
}
