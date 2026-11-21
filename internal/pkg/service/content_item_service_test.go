package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

type storeStub struct {
	lastCreate domain.CreateContentItemInput
	item       domain.ContentItem
}

func (s *storeStub) PingContext(context.Context) error {
	return nil
}

func (s *storeStub) CheckPublishedAttachment(context.Context, domain.PublishedAttachmentRef) error {
	return ErrNotFound
}

func (s *storeStub) SnapshotMaterials(context.Context, domain.MaterialSnapshotInput) (domain.MaterialSnapshot, error) {
	return domain.MaterialSnapshot{}, ErrNotFound
}

func (s *storeStub) ConfirmMaterialRead(context.Context, domain.ConfirmMaterialReadInput) error {
	return ErrNotFound
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

func (s *storeStub) Get(context.Context, string, string) (domain.ContentItem, error) {
	return s.item, nil
}

func (s *storeStub) List(context.Context, string) ([]domain.ContentItem, error) {
	return []domain.ContentItem{s.item}, nil
}

func (s *storeStub) ListCatalog(context.Context, domain.CatalogFilter, *domain.CatalogCursor) ([]domain.CatalogEntry, error) {
	return nil, nil
}

func (s *storeStub) Update(context.Context, domain.UpdateContentItemInput) (domain.ContentItem, error) {
	return s.item, nil
}

func (s *storeStub) ChangeState(context.Context, domain.ChangeContentItemStateInput, domain.ContentStatus) (domain.ContentItem, error) {
	return s.item, nil
}

func (s *storeStub) Delete(context.Context, string, string) (domain.ContentItem, error) {
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

func (s *storeStub) ListRevisions(context.Context, string, string) ([]domain.ContentRevision, error) {
	return nil, nil
}

func (s *storeStub) ListRevisionHistory(context.Context, domain.RevisionHistoryInput) ([]domain.RevisionSummary, error) {
	return nil, nil
}

func (s *storeStub) RestorePublishedRevision(context.Context, domain.RestorePublishedRevisionInput) (domain.ContentRevision, error) {
	return domain.ContentRevision{}, nil
}

func TestCreateAppliesDefaults(t *testing.T) {
	t.Parallel()

	store := &storeStub{}
	svc := NewContentItemService(store, nil)

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
	if store.lastCreate.Source != "  # Starter article  " {
		t.Error("Create changed source whitespace")
	}
}

func TestCreateRevisionValidatesDraftMetadata(t *testing.T) {
	t.Parallel()

	svc := NewContentItemService(&storeStub{}, nil)
	input := domain.CreateContentRevisionInput{
		ContentItemID: "item-1", ExpectedRevisionID: "c7da7413-aef0-4746-bf62-c80356f7e350",
		Source: "    code block\n\n",
	}
	for _, title := range []string{" ", strings.Repeat("a", 257), "Go\x00"} {
		input.DraftMetadata = &domain.DraftMetadata{Title: title}
		if _, err := svc.CreateRevision(t.Context(), input); !validator.IsValidationError(err) {
			t.Error("expected title validation error", err)
		}
	}
	input.DraftMetadata = &domain.DraftMetadata{Title: "Go", TopicIDs: []string{"not-a-uuid"}}
	if _, err := svc.CreateRevision(t.Context(), input); !validator.IsValidationError(err) {
		t.Error("expected topic validation error", err)
	}
	input.DraftMetadata.TopicIDs = nil
	revision, err := svc.CreateRevision(t.Context(), input)
	if err != nil || revision.Source != input.Source {
		t.Error("save did not preserve Markdown", err)
	}
}
