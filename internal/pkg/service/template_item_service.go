package service

import (
	"context"
	"strings"
	"time"

	"github.com/overmindv/bumblebee/internal/pkg/domain"
	"github.com/overmindv/bumblebee/internal/pkg/metrics"
	"github.com/overmindv/bumblebee/internal/pkg/validator"
)

type TemplateItemService struct {
	store     TemplateItemStore
	publisher TemplateItemEventPublisher
	metrics   *metrics.ServiceMetrics
}

func NewTemplateItemService(store TemplateItemStore, publisher TemplateItemEventPublisher, serviceMetrics *metrics.ServiceMetrics) *TemplateItemService {
	return &TemplateItemService{
		store:     store,
		publisher: publisher,
		metrics:   serviceMetrics,
	}
}

func (s *TemplateItemService) Store() TemplateItemStore {
	return s.store
}

func (s *TemplateItemService) Create(ctx context.Context, input domain.CreateTemplateItemInput) (item domain.TemplateItem, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "CreateTemplateItem", startedAt, err)
	}()

	normalized := normalizeCreateInput(input)
	if err = validator.ValidateCreateTemplateItem(normalized); err != nil {
		return domain.TemplateItem{}, err
	}

	item, err = s.store.Create(ctx, normalized)
	if err == nil {
		s.publisher.PublishCreated(ctx, item)
	}

	return item, err
}

func (s *TemplateItemService) Get(ctx context.Context, id string) (item domain.TemplateItem, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "GetTemplateItem", startedAt, err)
	}()

	if err = validator.ValidateID(id); err != nil {
		return domain.TemplateItem{}, err
	}

	return s.store.Get(ctx, strings.TrimSpace(id))
}

func (s *TemplateItemService) List(ctx context.Context) (items []domain.TemplateItem, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "ListTemplateItems", startedAt, err)
	}()

	return s.store.List(ctx)
}

func (s *TemplateItemService) Update(ctx context.Context, input domain.UpdateTemplateItemInput) (item domain.TemplateItem, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "UpdateTemplateItem", startedAt, err)
	}()

	normalized := normalizeUpdateInput(input)
	if err = validator.ValidateUpdateTemplateItem(normalized); err != nil {
		return domain.TemplateItem{}, err
	}

	item, err = s.store.Update(ctx, normalized)
	if err == nil {
		s.publisher.PublishUpdated(ctx, item)
	}

	return item, err
}

func (s *TemplateItemService) Delete(ctx context.Context, id string) (item domain.TemplateItem, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "DeleteTemplateItem", startedAt, err)
	}()

	if err = validator.ValidateID(id); err != nil {
		return domain.TemplateItem{}, err
	}

	item, err = s.store.Delete(ctx, strings.TrimSpace(id))
	if err == nil {
		s.publisher.PublishDeleted(ctx, item)
	}

	return item, err
}

func normalizeCreateInput(input domain.CreateTemplateItemInput) domain.CreateTemplateItemInput {
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "draft"
	}

	return domain.CreateTemplateItemInput{
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		Status:      strings.ToLower(status),
	}
}

func normalizeUpdateInput(input domain.UpdateTemplateItemInput) domain.UpdateTemplateItemInput {
	return domain.UpdateTemplateItemInput{
		ID:          strings.TrimSpace(input.ID),
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		Status:      strings.ToLower(strings.TrimSpace(input.Status)),
	}
}
