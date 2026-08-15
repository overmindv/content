package service

import (
	"context"
	"testing"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

type storeStub struct {
	lastCreate domain.CreateContentItemInput
	item       domain.ContentItem
}

func (s *storeStub) PingContext(context.Context) error {
	return nil
}

func (s *storeStub) Create(_ context.Context, input domain.CreateContentItemInput) (domain.ContentItem, error) {
	s.lastCreate = input
	s.item = domain.ContentItem{
		ID:          "item-1",
		Type:        input.Type,
		Status:      input.Status,
		Title:       input.Title,
		Slug:        input.Slug,
		Description: input.Description,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	return s.item, nil
}

func (s *storeStub) Get(context.Context, string) (domain.ContentItem, error) {
	return s.item, nil
}

func (s *storeStub) List(context.Context) ([]domain.ContentItem, error) {
	return []domain.ContentItem{s.item}, nil
}

func (s *storeStub) Update(context.Context, domain.UpdateContentItemInput) (domain.ContentItem, error) {
	return s.item, nil
}

func (s *storeStub) Delete(context.Context, string) (domain.ContentItem, error) {
	return s.item, nil
}

func (s *storeStub) CreateRevision(_ context.Context, input domain.CreateContentRevisionInput) (domain.ContentRevision, error) {
	return domain.ContentRevision{
		ID:            "revision-2",
		ContentItemID: input.ContentItemID,
		Revision:      2,
		Format:        input.Format,
		Source:        input.Source,
		CreatedAt:     time.Now().UTC(),
	}, nil
}

func (s *storeStub) ListRevisions(context.Context, string) ([]domain.ContentRevision, error) {
	return nil, nil
}

type publisherStub struct {
	created domain.ContentItem
}

func (p *publisherStub) PublishCreated(_ context.Context, item domain.ContentItem) {
	p.created = item
}

func (p *publisherStub) PublishUpdated(context.Context, domain.ContentItem) {}

func (p *publisherStub) PublishDeleted(context.Context, domain.ContentItem) {}

func (p *publisherStub) PublishRevisionCreated(context.Context, domain.ContentRevision) {}

func TestCreateAppliesDefaultsAndPublishesEvent(t *testing.T) {
	store := &storeStub{}
	publisher := &publisherStub{}
	serviceMetrics := metrics.New("content_test", prometheus.NewRegistry())
	svc := NewContentItemService(store, publisher, serviceMetrics)

	item, err := svc.Create(context.Background(), domain.CreateContentItemInput{
		Type:        domain.ContentTypeArticle,
		Title:       "  Starter article  ",
		Slug:        "starter-article",
		Description: "  Ready for content model  ",
		Source:      "  # Starter article  ",
		Tags:        []string{" Go ", "go"},
	})
	if err != nil {
		t.Errorf("Create() error = %v", err)
	}

	if store.lastCreate.Status != domain.ContentStatusDraft {
		t.Errorf("expected default status draft, got %q", store.lastCreate.Status)
	}

	if store.lastCreate.Format != domain.ContentFormatMarkdown {
		t.Errorf("expected default format markdown, got %q", store.lastCreate.Format)
	}

	if item.Title != "Starter article" {
		t.Errorf("expected trimmed title, got %q", item.Title)
	}

	if len(store.lastCreate.Tags) != 1 || store.lastCreate.Tags[0] != "go" {
		t.Errorf("expected normalized unique tag go, got %#v", store.lastCreate.Tags)
	}

	if publisher.created.ID != item.ID {
		t.Errorf("expected created event for item %q, got %q", item.ID, publisher.created.ID)
	}
}
