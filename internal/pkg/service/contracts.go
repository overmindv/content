package service

import (
	"context"
	"errors"

	"github.com/overmindv/bumblebee/internal/pkg/domain"
)

var ErrNotFound = errors.New("template item not found")

type TemplateItemStore interface {
	PingContext(ctx context.Context) error
	Create(ctx context.Context, input domain.CreateTemplateItemInput) (domain.TemplateItem, error)
	Get(ctx context.Context, id string) (domain.TemplateItem, error)
	List(ctx context.Context) ([]domain.TemplateItem, error)
	Update(ctx context.Context, input domain.UpdateTemplateItemInput) (domain.TemplateItem, error)
	Delete(ctx context.Context, id string) (domain.TemplateItem, error)
}

type TemplateItemEventPublisher interface {
	PublishCreated(ctx context.Context, item domain.TemplateItem)
	PublishUpdated(ctx context.Context, item domain.TemplateItem)
	PublishDeleted(ctx context.Context, item domain.TemplateItem)
}
