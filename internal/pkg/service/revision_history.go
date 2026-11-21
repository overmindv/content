package service

import (
	"context"
	"strings"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

func (s *ContentItemService) RevisionHistory(ctx context.Context, input domain.RevisionHistoryInput) ([]domain.RevisionSummary, int32, error) {
	input.ContentItemID = strings.ToLower(strings.TrimSpace(input.ContentItemID))
	switch {
	case !validProposalID(input.ContentItemID):
		return nil, 0, validator.Error{Message: "content_item_id must be a UUID"}
	case input.PageSize < 0 || input.PageSize > 50:
		return nil, 0, validator.Error{Message: "page size must be at most 50"}
	case input.BeforeRevision < 0:
		return nil, 0, validator.Error{Message: "before_revision must be non-negative"}
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	revisions, err := s.store.ListRevisionHistory(ctx, input)
	if err != nil {
		return nil, 0, err
	}
	if len(revisions) <= int(input.PageSize) {
		return revisions, 0, nil
	}
	revisions = revisions[:input.PageSize]

	return revisions, revisions[len(revisions)-1].Revision, nil
}
