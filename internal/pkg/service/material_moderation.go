package service

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

func (s *ContentItemService) GetMaterialModeration(ctx context.Context, id string) (domain.MaterialModeration, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !validProposalID(id) {
		return domain.MaterialModeration{}, validator.Error{Message: "material_id must be a UUID"}
	}

	return s.store.GetMaterialModeration(ctx, id)
}

func (s *ContentItemService) SetMaterialModeration(ctx context.Context, input domain.SetMaterialModerationInput) (domain.MaterialModeration, error) {
	input.MaterialID = strings.ToLower(strings.TrimSpace(input.MaterialID))
	input.PublishedRevisionID = strings.ToLower(strings.TrimSpace(input.PublishedRevisionID))
	if !validProposalID(input.MaterialID) {
		return domain.MaterialModeration{}, validator.Error{Message: "material_id must be a UUID"}
	}
	if !validProposalID(input.PublishedRevisionID) {
		return domain.MaterialModeration{}, validator.Error{Message: "expected_published_revision_id must be a UUID"}
	}
	if input.Version < 0 {
		return domain.MaterialModeration{}, validator.Error{Message: "expected_moderation_version must be non-negative"}
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Reason) == 0 || len(input.Reason) > 1024 || !utf8.ValidString(input.Reason) ||
		strings.IndexFunc(input.Reason, unicode.IsControl) >= 0 {
		return domain.MaterialModeration{}, validator.Error{Message: "reason must contain 1 to 1024 bytes of text without control characters"}
	}

	return s.store.SetMaterialModeration(ctx, input)
}
