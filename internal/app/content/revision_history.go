package content

import (
	"context"

	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) ListContentRevisionHistory(ctx context.Context, req *contentpb.ListContentRevisionHistoryRequest) (*contentpb.ListContentRevisionHistoryResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	history, next, err := s.itemService.RevisionHistory(ctx, domain.RevisionHistoryInput{
		ContentItemID: req.GetContentItemId(), ActorID: actor, PageSize: req.GetPageSize(), BeforeRevision: req.GetBeforeRevision(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	items := make([]*contentpb.ContentRevisionSummary, 0, len(history))
	for _, revision := range history {
		items = append(items, &contentpb.ContentRevisionSummary{
			Id: revision.ID, Revision: revision.Revision, Format: string(revision.Format), Message: revision.Message,
			CreatedBy: revision.CreatedBy, CreatedAt: timestamppb.New(revision.CreatedAt),
		})
	}

	return &contentpb.ListContentRevisionHistoryResponse{Items: items, NextBeforeRevision: next}, nil
}
