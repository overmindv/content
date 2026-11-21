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
	ErrNotFound         = errors.New("content item not found")
	ErrRevisionConflict = errors.New("current revision changed")
	ErrInvalidState     = errors.New("operation is not allowed in this content state")
	ErrProposalNotFound = errors.New("change proposal not found")
	ErrProposalExists   = errors.New("an open proposal already exists for this material and author")
)

type ContentItemStore interface {
	PingContext(ctx context.Context) error
	Create(ctx context.Context, input domain.CreateContentItemInput) (domain.ContentItem, error)
	Get(ctx context.Context, id, actorID string) (domain.ContentItem, error)
	List(ctx context.Context, actorID string) ([]domain.ContentItem, error)
	Update(ctx context.Context, input domain.UpdateContentItemInput) (domain.ContentItem, error)
	ChangeState(ctx context.Context, input domain.ChangeContentItemStateInput, target domain.ContentStatus) (domain.ContentItem, error)
	Delete(ctx context.Context, id, actorID string) (domain.ContentItem, error)
	CreateRevision(ctx context.Context, input domain.CreateContentRevisionInput) (domain.ContentRevision, error)
	ListRevisions(ctx context.Context, contentItemID, actorID string) ([]domain.ContentRevision, error)
}

type ChangeProposalStore interface {
	OpenChangeProposal(context.Context, domain.OpenChangeProposalInput) (domain.ChangeProposal, error)
	GetChangeProposal(ctx context.Context, id, actorID string) (domain.ChangeProposal, error)
	AppendChangeProposalRevision(context.Context, domain.AppendChangeProposalRevisionInput) (domain.ChangeProposal, error)
	CloseChangeProposal(ctx context.Context, id, actorID string) (domain.ChangeProposal, error)
}
