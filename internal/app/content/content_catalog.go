package content

import (
	"context"

	"github.com/overmindv/content/internal/app/content/mapper"
	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) ListContentCatalog(ctx context.Context, req *contentpb.ListContentCatalogRequest) (*contentpb.ListContentCatalogResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	items, next, err := s.itemService.Catalog(ctx, domain.CatalogFilter{
		ActorID: actor, TopicID: req.GetTopicId(), AuthorID: req.GetAuthorId(),
		Status: domain.ContentStatus(req.GetStatus()), PageSize: req.GetPageSize(),
	}, req.GetCursor())
	if err != nil {
		return nil, toStatusError(err)
	}
	entries := make([]*contentpb.ContentCatalogEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, &contentpb.ContentCatalogEntry{
			Id: item.ID, Title: item.Title, Slug: item.Slug, Status: string(item.Status),
			AuthorId: item.AuthorID, CreatedAt: timestamppb.New(item.CreatedAt), RevisionId: item.RevisionID,
			Learning: mapper.ToProtoLearning(&item.Learning),
		})
	}
	return &contentpb.ListContentCatalogResponse{Entries: entries, NextCursor: next}, nil
}
