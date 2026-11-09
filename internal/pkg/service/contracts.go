package service

import (
	"context"
	"errors"

	"github.com/overmindv/content/internal/pkg/domain"
)

// Публикация событий контента атомарно выполняется хранилищем через transaction
// outbox (в той же транзакции, что и бизнес-изменение), поэтому сервис не держит
// собственного publisher-контракта.

var ErrNotFound = errors.New("content item not found")

type ContentItemStore interface {
	PingContext(ctx context.Context) error
	Create(ctx context.Context, input domain.CreateContentItemInput) (domain.ContentItem, error)
	Get(ctx context.Context, id string) (domain.ContentItem, error)
	List(ctx context.Context) ([]domain.ContentItem, error)
	Update(ctx context.Context, input domain.UpdateContentItemInput) (domain.ContentItem, error)
	Delete(ctx context.Context, id string) (domain.ContentItem, error)
	CreateRevision(ctx context.Context, input domain.CreateContentRevisionInput) (domain.ContentRevision, error)
	ListRevisions(ctx context.Context, contentItemID string) ([]domain.ContentRevision, error)
}
