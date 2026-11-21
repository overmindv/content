package content

import (
	"context"

	"github.com/overmindv/content/internal/app/content/mapper"
	contentpb "github.com/overmindv/content/internal/pkg/api/content"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

type Server struct {
	contentpb.UnimplementedContentServiceServer
	itemService *service.ContentItemService
	proposals   *service.ChangeProposalService
	review      *service.ChangeReviewService
}

func (s *Server) PublishContentItem(ctx context.Context, req *contentpb.PublishContentItemRequest) (*contentpb.PublishContentItemResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	item, err := s.itemService.Publish(ctx, domain.ChangeContentItemStateInput{
		ID: req.GetId(), ExpectedRevisionID: req.GetExpectedRevisionId(), ActorID: actor,
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.PublishContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) ArchiveContentItem(ctx context.Context, req *contentpb.ArchiveContentItemRequest) (*contentpb.ArchiveContentItemResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	item, err := s.itemService.Archive(ctx, domain.ChangeContentItemStateInput{
		ID: req.GetId(), ExpectedRevisionID: req.GetExpectedRevisionId(), ActorID: actor,
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.ArchiveContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func NewServer(itemService *service.ContentItemService, proposals *service.ChangeProposalService, review *service.ChangeReviewService) *Server {
	return &Server{itemService: itemService, proposals: proposals, review: review}
}

func (s *Server) CreateContentItem(ctx context.Context, req *contentpb.CreateContentItemRequest) (*contentpb.CreateContentItemResponse, error) {
	id, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	item, err := s.itemService.Create(ctx, mapper.ToCreateInput(mapper.CreateContentItemFromProto(req, id)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.CreateContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) GetContentItem(ctx context.Context, req *contentpb.GetContentItemRequest) (*contentpb.GetContentItemResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	item, err := s.itemService.Get(ctx, req.GetId(), actor)
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.GetContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) ListContentItems(ctx context.Context, _ *contentpb.ListContentItemsRequest) (*contentpb.ListContentItemsResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.itemService.List(ctx, actor)
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.ListContentItemsResponse{Items: mapper.ToProtoList(items)}, nil
}

func (s *Server) UpdateContentItem(ctx context.Context, req *contentpb.UpdateContentItemRequest) (*contentpb.UpdateContentItemResponse, error) {
	id, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	item, err := s.itemService.Update(ctx, mapper.ToUpdateInput(mapper.UpdateContentItemFromProto(req, id)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.UpdateContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) DeleteContentItem(ctx context.Context, req *contentpb.DeleteContentItemRequest) (*contentpb.DeleteContentItemResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	item, err := s.itemService.Delete(ctx, req.GetId(), actor)
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.DeleteContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) CreateContentRevision(ctx context.Context, req *contentpb.CreateContentRevisionRequest) (*contentpb.CreateContentRevisionResponse, error) {
	id, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	revision, err := s.itemService.CreateRevision(ctx, mapper.ToCreateRevisionInput(mapper.CreateContentRevisionFromProto(req, id)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.CreateContentRevisionResponse{Revision: mapper.ToProtoRevision(revision)}, nil
}

func (s *Server) ListContentRevisions(ctx context.Context, req *contentpb.ListContentRevisionsRequest) (*contentpb.ListContentRevisionsResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	revisions, err := s.itemService.ListRevisions(ctx, req.GetContentItemId(), actor)
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.ListContentRevisionsResponse{Revisions: mapper.ToProtoRevisions(revisions)}, nil
}
