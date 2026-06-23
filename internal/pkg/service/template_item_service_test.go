package service

import (
	"context"
	"testing"
	"time"

	"github.com/overmindv/bumblebee/internal/pkg/domain"
	"github.com/overmindv/bumblebee/internal/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

type storeStub struct {
	lastCreate domain.CreateTemplateItemInput
	item       domain.TemplateItem
}

func (s *storeStub) PingContext(context.Context) error {
	return nil
}

func (s *storeStub) Create(_ context.Context, input domain.CreateTemplateItemInput) (domain.TemplateItem, error) {
	s.lastCreate = input
	s.item = domain.TemplateItem{
		ID:          "item-1",
		Name:        input.Name,
		Description: input.Description,
		Status:      input.Status,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	return s.item, nil
}

func (s *storeStub) Get(context.Context, string) (domain.TemplateItem, error) {
	return s.item, nil
}

func (s *storeStub) List(context.Context) ([]domain.TemplateItem, error) {
	return []domain.TemplateItem{s.item}, nil
}

func (s *storeStub) Update(context.Context, domain.UpdateTemplateItemInput) (domain.TemplateItem, error) {
	return s.item, nil
}

func (s *storeStub) Delete(context.Context, string) (domain.TemplateItem, error) {
	return s.item, nil
}

type publisherStub struct {
	created domain.TemplateItem
}

func (p *publisherStub) PublishCreated(_ context.Context, item domain.TemplateItem) {
	p.created = item
}

func (p *publisherStub) PublishUpdated(context.Context, domain.TemplateItem) {}

func (p *publisherStub) PublishDeleted(context.Context, domain.TemplateItem) {}

func TestCreateAppliesDefaultStatusAndPublishesEvent(t *testing.T) {
	store := &storeStub{}
	publisher := &publisherStub{}
	serviceMetrics := metrics.New("bumblebee_test", prometheus.NewRegistry())
	svc := NewTemplateItemService(store, publisher, serviceMetrics)

	item, err := svc.Create(context.Background(), domain.CreateTemplateItemInput{
		Name:        "  Starter item  ",
		Description: "  Ready for copy  ",
	})
	if err != nil {
		t.Errorf("Create() error = %v", err)
	}

	if store.lastCreate.Status != "draft" {
		t.Errorf("expected default status draft, got %q", store.lastCreate.Status)
	}

	if item.Name != "Starter item" {
		t.Errorf("expected trimmed name, got %q", item.Name)
	}

	if publisher.created.ID != item.ID {
		t.Errorf("expected created event for item %q, got %q", item.ID, publisher.created.ID)
	}
}
