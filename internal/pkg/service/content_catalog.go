package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

func (s *ContentItemService) Catalog(ctx context.Context, filter domain.CatalogFilter, token string) ([]domain.CatalogEntry, string, error) {
	if filter.PageSize < 0 || filter.PageSize > 50 {
		return nil, "", validator.Error{Message: "page size must be at most 50"}
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}
	switch filter.Status {
	case "", domain.ContentStatusDraft, domain.ContentStatusPublished, domain.ContentStatusArchived:
	default:
		return nil, "", validator.Error{Message: "invalid content status filter"}
	}
	var err error
	if filter.TopicID, err = optionalCatalogUUID(filter.TopicID); err != nil {
		return nil, "", err
	}
	if filter.AuthorID, err = optionalCatalogUUID(filter.AuthorID); err != nil {
		return nil, "", err
	}
	cursor, err := decodeCatalogCursor(token)
	if err != nil {
		return nil, "", err
	}
	items, err := s.store.ListCatalog(ctx, filter, cursor)
	if err != nil {
		return nil, "", err
	}
	if len(items) <= int(filter.PageSize) {
		return items, "", nil
	}
	items = items[:filter.PageSize]
	last := items[len(items)-1]
	data, err := json.Marshal(domain.CatalogCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	if err != nil {
		return nil, "", err
	}

	return items, base64.RawURLEncoding.EncodeToString(data), nil
}

func (s *ContentItemService) validateLearningMetadata(ctx context.Context, input *domain.LearningMetadata) (*domain.LearningMetadata, error) {
	if input == nil {
		return nil, nil
	}
	if len(input.TopicIDs) > 8 || len(input.LearningOutcomes) > 8 || input.EstimatedMinutes < 0 || input.EstimatedMinutes > 480 {
		return nil, validator.Error{Message: "learning metadata supports up to 8 topics, 8 outcomes and 480 minutes"}
	}
	learning := &domain.LearningMetadata{EstimatedMinutes: input.EstimatedMinutes}
	seen := make(map[string]struct{}, len(input.TopicIDs))
	for _, rawID := range input.TopicIDs {
		id, err := optionalCatalogUUID(rawID)
		if err != nil || id == "" {
			return nil, validator.Error{Message: "each topic_id must be a UUID"}
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		learning.TopicIDs = append(learning.TopicIDs, id)
	}
	for _, raw := range input.LearningOutcomes {
		outcome := strings.TrimSpace(raw)
		if outcome == "" || len(raw) > 256 || !utf8.ValidString(raw) || strings.IndexByte(raw, 0) >= 0 {
			return nil, validator.Error{Message: "learning outcomes must be UTF-8 text without NUL, up to 256 bytes each"}
		}
		learning.LearningOutcomes = append(learning.LearningOutcomes, outcome)
	}
	if len(learning.TopicIDs) == 0 {
		return learning, nil
	}
	if s.topics == nil {
		return nil, ErrTopicCatalogUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for _, id := range learning.TopicIDs {
		if err := s.topics.CheckActiveTopic(ctx, id); err != nil {
			return nil, err
		}
	}

	return learning, nil
}

func optionalCatalogUUID(input string) (string, error) {
	if input == "" {
		return "", nil
	}
	id, err := uuid.Parse(input)
	if len(input) != 36 || err != nil || id == uuid.Nil {
		return "", validator.Error{Message: "topic_id and author_id must be UUIDs"}
	}

	return id.String(), nil
}

func decodeCatalogCursor(token string) (*domain.CatalogCursor, error) {
	if token == "" {
		return nil, nil
	}
	invalid := validator.Error{Message: "invalid catalog cursor"}
	if len(token) > 256 {
		return nil, invalid
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, invalid
	}
	var cursor domain.CatalogCursor
	if err = json.Unmarshal(data, &cursor); err != nil || cursor.CreatedAt.IsZero() || !validProposalID(cursor.ID) {
		return nil, invalid
	}

	return &cursor, nil
}
