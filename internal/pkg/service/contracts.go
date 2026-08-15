package service

import (
	"context"
	"errors"

	"github.com/overmindv/content/internal/pkg/domain"
)

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

type ContentItemEventPublisher interface {
	PublishCreated(ctx context.Context, item domain.ContentItem)
	PublishUpdated(ctx context.Context, item domain.ContentItem)
	PublishDeleted(ctx context.Context, item domain.ContentItem)
	PublishRevisionCreated(ctx context.Context, revision domain.ContentRevision)
}
