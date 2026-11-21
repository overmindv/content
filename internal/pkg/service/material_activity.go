package service

import (
	"context"
	"strings"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

func (s *ContentItemService) SnapshotMaterials(ctx context.Context, input domain.MaterialSnapshotInput) (domain.MaterialSnapshot, error) {
	if input.PageSize < 0 || input.PageSize > 100 {
		return domain.MaterialSnapshot{}, validator.Error{Message: "page_size must not exceed 100"}
	}
	if input.Watermark != nil && *input.Watermark < 0 {
		return domain.MaterialSnapshot{}, validator.Error{Message: "invalid watermark"}
	}
	if input.AfterID != "" && (!validProposalID(input.AfterID) || input.Watermark == nil) {
		return domain.MaterialSnapshot{}, validator.Error{Message: "after_id requires a UUID and watermark"}
	}
	if input.PageSize == 0 {
		input.PageSize = 50
	}

	return s.store.SnapshotMaterials(ctx, input)
}

func (s *ContentItemService) ConfirmMaterialRead(ctx context.Context, input domain.ConfirmMaterialReadInput) error {
	if !validProposalID(input.MaterialID) || !validProposalID(input.RevisionID) || !validProposalID(input.ActorID) {
		return validator.Error{Message: "material_id, revision_id and actor must be UUIDs"}
	}
	input.MaterialID = strings.ToLower(input.MaterialID)
	input.RevisionID = strings.ToLower(input.RevisionID)
	input.ActorID = strings.ToLower(input.ActorID)

	return s.store.ConfirmMaterialRead(ctx, input)
}
