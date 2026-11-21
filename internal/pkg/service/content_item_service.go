package service

import (
	"context"
	"strings"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

type ContentItemService struct {
	store  ContentItemStore
	topics TopicChecker
	media  AttachmentChecker
}

func NewContentItemService(store ContentItemStore, topics TopicChecker) *ContentItemService {
	return &ContentItemService{
		store:  store,
		topics: topics,
	}
}

func (s *ContentItemService) Store() ContentItemStore {
	return s.store
}

func (s *ContentItemService) SetAttachmentChecker(checker AttachmentChecker) { s.media = checker }

func (s *ContentItemService) Create(ctx context.Context, input domain.CreateContentItemInput) (item domain.ContentItem, err error) {
	if err = validator.ValidateSourceSize(len(input.Source)); err != nil {
		return domain.ContentItem{}, err
	}
	if err = validator.ValidateAssetCount(len(input.Assets)); err != nil {
		return domain.ContentItem{}, err
	}
	normalized := normalizeCreateInput(input)
	if err = validator.ValidateCreateContentItem(normalized); err != nil {
		return domain.ContentItem{}, err
	}
	if normalized.Learning, err = s.validateLearningMetadata(ctx, input.Learning); err != nil {
		return domain.ContentItem{}, err
	}
	if len(normalized.Assets) != 0 {
		set, checkErr := checkAttachmentSet(ctx, s.media, normalized.CreatedBy, &domain.AttachmentSet{Assets: normalized.Assets})
		if checkErr != nil {
			return domain.ContentItem{}, checkErr
		}
		normalized.Assets = set.Assets
	}

	item, err = s.store.Create(ctx, normalized)

	return item, err
}

func (s *ContentItemService) Get(ctx context.Context, id, actorID string) (item domain.ContentItem, err error) {
	if err = validator.ValidateID(id); err != nil {
		return domain.ContentItem{}, err
	}

	return s.store.Get(ctx, strings.TrimSpace(id), actorID)
}

func (s *ContentItemService) List(ctx context.Context, actorID string) (items []domain.ContentItem, err error) {
	return s.store.List(ctx, actorID)
}

func (s *ContentItemService) GetPublished(ctx context.Context, id string) (domain.ContentItem, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !validProposalID(id) {
		return domain.ContentItem{}, validator.Error{Message: "content_item_id must be a UUID"}
	}
	// A published read must never select the owner's working revision.
	return s.store.Get(ctx, id, "")
}

func (s *ContentItemService) Update(ctx context.Context, input domain.UpdateContentItemInput) (item domain.ContentItem, err error) {
	normalized := normalizeUpdateInput(input)
	if err = validator.ValidateUpdateContentItem(normalized); err != nil {
		return domain.ContentItem{}, err
	}
	if normalized.Learning, err = s.validateLearningMetadata(ctx, input.Learning); err != nil {
		return domain.ContentItem{}, err
	}

	item, err = s.store.Update(ctx, normalized)

	return item, err
}

func (s *ContentItemService) Publish(ctx context.Context, input domain.ChangeContentItemStateInput) (domain.ContentItem, error) {
	return s.changeState(ctx, input, domain.ContentStatusPublished)
}

func (s *ContentItemService) Archive(ctx context.Context, input domain.ChangeContentItemStateInput) (domain.ContentItem, error) {
	return s.changeState(ctx, input, domain.ContentStatusArchived)
}

func (s *ContentItemService) changeState(ctx context.Context, input domain.ChangeContentItemStateInput, target domain.ContentStatus) (domain.ContentItem, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.ExpectedRevisionID = strings.ToLower(strings.TrimSpace(input.ExpectedRevisionID))
	if err := validator.ValidateID(input.ID); err != nil {
		return domain.ContentItem{}, err
	}
	if err := validator.ValidateExpectedRevisionID(input.ExpectedRevisionID); err != nil {
		return domain.ContentItem{}, err
	}

	return s.store.ChangeState(ctx, input, target)
}

func (s *ContentItemService) Delete(ctx context.Context, id, actorID string) (item domain.ContentItem, err error) {
	if err = validator.ValidateID(id); err != nil {
		return domain.ContentItem{}, err
	}

	item, err = s.store.Delete(ctx, strings.TrimSpace(id), actorID)

	return item, err
}

func (s *ContentItemService) CreateRevision(ctx context.Context, input domain.CreateContentRevisionInput) (revision domain.ContentRevision, err error) {
	if err = validator.ValidateSourceSize(len(input.Source)); err != nil {
		return domain.ContentRevision{}, err
	}
	normalized := normalizeCreateRevisionInput(input)
	if err = validator.ValidateCreateContentRevision(normalized); err != nil {
		return domain.ContentRevision{}, err
	}
	normalized.Attachments, err = checkAttachmentSet(ctx, s.media, normalized.CreatedBy, input.Attachments)
	if err != nil {
		return domain.ContentRevision{}, err
	}

	revision, err = s.store.CreateRevision(ctx, normalized)

	return revision, err
}

func (s *ContentItemService) ListRevisions(ctx context.Context, contentItemID, actorID string) (revisions []domain.ContentRevision, err error) {
	if err = validator.ValidateID(contentItemID); err != nil {
		return nil, err
	}

	return s.store.ListRevisions(ctx, strings.TrimSpace(contentItemID), actorID)
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
	status := domain.ContentStatus(strings.ToLower(strings.TrimSpace(string(input.Status))))
	if status == "" {
		status = domain.ContentStatusDraft
	}

	return domain.UpdateContentItemInput{
		ID:          strings.TrimSpace(input.ID),
		Type:        domain.ContentType(strings.ToLower(strings.TrimSpace(string(input.Type)))),
		Status:      status,
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
		ContentItemID:      strings.TrimSpace(input.ContentItemID),
		ExpectedRevisionID: strings.ToLower(strings.TrimSpace(input.ExpectedRevisionID)),
		Format:             domain.ContentFormat(strings.ToLower(strings.TrimSpace(string(format)))),
		Source:             strings.TrimSpace(input.Source),
		Message:            strings.TrimSpace(input.Message),
		CreatedBy:          strings.TrimSpace(input.CreatedBy),
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
