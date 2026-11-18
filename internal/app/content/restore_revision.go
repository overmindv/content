package content

import (
	"context"

	"github.com/overmindv/content/internal/app/content/mapper"
	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
)

func (s *Server) RestorePublishedRevision(ctx context.Context, req *contentpb.RestorePublishedRevisionRequest) (*contentpb.RestorePublishedRevisionResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	revision, err := s.itemService.RestorePublishedRevision(ctx, domain.RestorePublishedRevisionInput{
		ContentItemID: req.GetContentItemId(), ExpectedPublishedID: req.GetExpectedPublishedRevisionId(),
		SourceRevisionID: req.GetSourceRevisionId(), ActorID: actor,
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	return &contentpb.RestorePublishedRevisionResponse{Revision: mapper.ToProtoRevision(revision)}, nil
}
