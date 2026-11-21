package service

import (
	"context"
	"strings"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

func (s *ContentItemService) RestorePublishedRevision(ctx context.Context, input domain.RestorePublishedRevisionInput) (domain.ContentRevision, error) {
	input.ContentItemID = strings.ToLower(strings.TrimSpace(input.ContentItemID))
	input.ExpectedPublishedID = strings.ToLower(strings.TrimSpace(input.ExpectedPublishedID))
	input.SourceRevisionID = strings.ToLower(strings.TrimSpace(input.SourceRevisionID))
	if !validProposalID(input.ContentItemID) {
		return domain.ContentRevision{}, validator.Error{Message: "content_item_id must be a UUID"}
	}
	if !validProposalID(input.ExpectedPublishedID) {
		return domain.ContentRevision{}, validator.Error{Message: "expected_published_revision_id must be a UUID"}
	}
	if !validProposalID(input.SourceRevisionID) {
		return domain.ContentRevision{}, validator.Error{Message: "source_revision_id must be a UUID"}
	}

	return s.store.RestorePublishedRevision(ctx, input)
}
