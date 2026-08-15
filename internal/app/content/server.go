package content

import (
	"context"

	"github.com/overmindv/content/internal/app/content/mapper"
	contentpb "github.com/overmindv/content/internal/pkg/api/content"
	"github.com/overmindv/content/internal/pkg/service"
)

type Server struct {
	contentpb.UnimplementedContentServiceServer
	itemService *service.ContentItemService
}

func NewServer(itemService *service.ContentItemService) *Server {
	return &Server{itemService: itemService}
}

func (s *Server) CreateContentItem(ctx context.Context, req *contentpb.CreateContentItemRequest) (*contentpb.CreateContentItemResponse, error) {
	item, err := s.itemService.Create(ctx, mapper.ToCreateInput(mapper.CreateContentItemFromProto(req)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.CreateContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) GetContentItem(ctx context.Context, req *contentpb.GetContentItemRequest) (*contentpb.GetContentItemResponse, error) {
	item, err := s.itemService.Get(ctx, req.GetId())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.GetContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) ListContentItems(ctx context.Context, _ *contentpb.ListContentItemsRequest) (*contentpb.ListContentItemsResponse, error) {
	items, err := s.itemService.List(ctx)
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.ListContentItemsResponse{Items: mapper.ToProtoList(items)}, nil
}

func (s *Server) UpdateContentItem(ctx context.Context, req *contentpb.UpdateContentItemRequest) (*contentpb.UpdateContentItemResponse, error) {
	item, err := s.itemService.Update(ctx, mapper.ToUpdateInput(mapper.UpdateContentItemFromProto(req)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.UpdateContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) DeleteContentItem(ctx context.Context, req *contentpb.DeleteContentItemRequest) (*contentpb.DeleteContentItemResponse, error) {
	item, err := s.itemService.Delete(ctx, req.GetId())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.DeleteContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) CreateContentRevision(ctx context.Context, req *contentpb.CreateContentRevisionRequest) (*contentpb.CreateContentRevisionResponse, error) {
	revision, err := s.itemService.CreateRevision(ctx, mapper.ToCreateRevisionInput(mapper.CreateContentRevisionFromProto(req)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.CreateContentRevisionResponse{Revision: mapper.ToProtoRevision(revision)}, nil
}

func (s *Server) ListContentRevisions(ctx context.Context, req *contentpb.ListContentRevisionsRequest) (*contentpb.ListContentRevisionsResponse, error) {
	revisions, err := s.itemService.ListRevisions(ctx, req.GetContentItemId())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.ListContentRevisionsResponse{Revisions: mapper.ToProtoRevisions(revisions)}, nil
}
