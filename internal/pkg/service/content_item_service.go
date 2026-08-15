package service

import (
	"context"
	"strings"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/metrics"
	"github.com/overmindv/content/internal/pkg/validator"
)

type ContentItemService struct {
	store     ContentItemStore
	publisher ContentItemEventPublisher
	metrics   *metrics.ServiceMetrics
}

func NewContentItemService(store ContentItemStore, publisher ContentItemEventPublisher, serviceMetrics *metrics.ServiceMetrics) *ContentItemService {
	return &ContentItemService{
		store:     store,
		publisher: publisher,
		metrics:   serviceMetrics,
	}
}

func (s *ContentItemService) Store() ContentItemStore {
	return s.store
}

func (s *ContentItemService) Create(ctx context.Context, input domain.CreateContentItemInput) (item domain.ContentItem, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "CreateContentItem", startedAt, err)
	}()

	normalized := normalizeCreateInput(input)
	if err = validator.ValidateCreateContentItem(normalized); err != nil {
		return domain.ContentItem{}, err
	}

	item, err = s.store.Create(ctx, normalized)
	if err == nil {
		s.publisher.PublishCreated(ctx, item)
	}

	return item, err
}

func (s *ContentItemService) Get(ctx context.Context, id string) (item domain.ContentItem, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "GetContentItem", startedAt, err)
	}()

	if err = validator.ValidateID(id); err != nil {
		return domain.ContentItem{}, err
	}

	return s.store.Get(ctx, strings.TrimSpace(id))
}

func (s *ContentItemService) List(ctx context.Context) (items []domain.ContentItem, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "ListContentItems", startedAt, err)
	}()

	return s.store.List(ctx)
}

func (s *ContentItemService) Update(ctx context.Context, input domain.UpdateContentItemInput) (item domain.ContentItem, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "UpdateContentItem", startedAt, err)
	}()

	normalized := normalizeUpdateInput(input)
	if err = validator.ValidateUpdateContentItem(normalized); err != nil {
		return domain.ContentItem{}, err
	}

	item, err = s.store.Update(ctx, normalized)
	if err == nil {
		s.publisher.PublishUpdated(ctx, item)
	}

	return item, err
}

func (s *ContentItemService) Delete(ctx context.Context, id string) (item domain.ContentItem, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "DeleteContentItem", startedAt, err)
	}()

	if err = validator.ValidateID(id); err != nil {
		return domain.ContentItem{}, err
	}

	item, err = s.store.Delete(ctx, strings.TrimSpace(id))
	if err == nil {
		s.publisher.PublishDeleted(ctx, item)
	}

	return item, err
}

func (s *ContentItemService) CreateRevision(ctx context.Context, input domain.CreateContentRevisionInput) (revision domain.ContentRevision, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "CreateContentRevision", startedAt, err)
	}()

	normalized := normalizeCreateRevisionInput(input)
	if err = validator.ValidateCreateContentRevision(normalized); err != nil {
		return domain.ContentRevision{}, err
	}

	revision, err = s.store.CreateRevision(ctx, normalized)
	if err == nil {
		s.publisher.PublishRevisionCreated(ctx, revision)
	}

	return revision, err
}

func (s *ContentItemService) ListRevisions(ctx context.Context, contentItemID string) (revisions []domain.ContentRevision, err error) {
	startedAt := time.Now()
	defer func() {
		s.metrics.ObserveRequest("grpc", "ListContentRevisions", startedAt, err)
	}()

	if err = validator.ValidateID(contentItemID); err != nil {
		return nil, err
	}

	return s.store.ListRevisions(ctx, strings.TrimSpace(contentItemID))
}

func normalizeCreateInput(input domain.CreateContentItemInput) domain.CreateContentItemInput {
	status := input.Status
	if status == "" {
		status = domain.ContentStatusDraft
	}

	format := input.Format
	if format == "" {
		format = domain.ContentFormatMarkdown
	}

	return domain.CreateContentItemInput{
		Type:        domain.ContentType(strings.ToLower(strings.TrimSpace(string(input.Type)))),
		Status:      domain.ContentStatus(strings.ToLower(strings.TrimSpace(string(status)))),
		Title:       strings.TrimSpace(input.Title),
		Slug:        strings.ToLower(strings.TrimSpace(input.Slug)),
		Description: strings.TrimSpace(input.Description),
		Format:      domain.ContentFormat(strings.ToLower(strings.TrimSpace(string(format)))),
		Source:      strings.TrimSpace(input.Source),
		Message:     strings.TrimSpace(input.Message),
		CreatedBy:   strings.TrimSpace(input.CreatedBy),
		Tags:        normalizeTags(input.Tags),
		Assets:      normalizeAssets(input.Assets),
	}
}

func normalizeUpdateInput(input domain.UpdateContentItemInput) domain.UpdateContentItemInput {
	return domain.UpdateContentItemInput{
		ID:          strings.TrimSpace(input.ID),
		Type:        domain.ContentType(strings.ToLower(strings.TrimSpace(string(input.Type)))),
		Status:      domain.ContentStatus(strings.ToLower(strings.TrimSpace(string(input.Status)))),
		Title:       strings.TrimSpace(input.Title),
		Slug:        strings.ToLower(strings.TrimSpace(input.Slug)),
		Description: strings.TrimSpace(input.Description),
		UpdatedBy:   strings.TrimSpace(input.UpdatedBy),
	}
}

func normalizeCreateRevisionInput(input domain.CreateContentRevisionInput) domain.CreateContentRevisionInput {
	format := input.Format
	if format == "" {
		format = domain.ContentFormatMarkdown
	}

	return domain.CreateContentRevisionInput{
		ContentItemID: strings.TrimSpace(input.ContentItemID),
		Format:        domain.ContentFormat(strings.ToLower(strings.TrimSpace(string(format)))),
		Source:        strings.TrimSpace(input.Source),
		Message:       strings.TrimSpace(input.Message),
		CreatedBy:     strings.TrimSpace(input.CreatedBy),
	}
}

func normalizeTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		if normalized == "" {
			result = append(result, normalized)
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}

		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}

	return result
}

func normalizeAssets(assets []domain.CreateContentAssetInput) []domain.CreateContentAssetInput {
	result := make([]domain.CreateContentAssetInput, 0, len(assets))
	for _, asset := range assets {
		result = append(result, domain.CreateContentAssetInput{
			AssetID:  strings.TrimSpace(asset.AssetID),
			Kind:     domain.AssetKind(strings.ToLower(strings.TrimSpace(string(asset.Kind)))),
			Title:    strings.TrimSpace(asset.Title),
			Position: asset.Position,
		})
	}

	return result
}
