package bumblebee

import (
	"context"

	"github.com/overmindv/bumblebee/internal/app/bumblebee/mapper"
	bumblebeepb "github.com/overmindv/bumblebee/internal/pkg/api/bumblebee"
	"github.com/overmindv/bumblebee/internal/pkg/service"
)

type Server struct {
	bumblebeepb.UnimplementedBumblebeeServiceServer
	itemService *service.ContentItemService
}

func NewServer(itemService *service.ContentItemService) *Server {
	return &Server{itemService: itemService}
}

func (s *Server) CreateContentItem(ctx context.Context, req *bumblebeepb.CreateContentItemRequest) (*bumblebeepb.CreateContentItemResponse, error) {
	item, err := s.itemService.Create(ctx, mapper.ToCreateInput(mapper.CreateContentItemFromProto(req)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.CreateContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) GetContentItem(ctx context.Context, req *bumblebeepb.GetContentItemRequest) (*bumblebeepb.GetContentItemResponse, error) {
	item, err := s.itemService.Get(ctx, req.GetId())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.GetContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) ListContentItems(ctx context.Context, _ *bumblebeepb.ListContentItemsRequest) (*bumblebeepb.ListContentItemsResponse, error) {
	items, err := s.itemService.List(ctx)
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.ListContentItemsResponse{Items: mapper.ToProtoList(items)}, nil
}

func (s *Server) UpdateContentItem(ctx context.Context, req *bumblebeepb.UpdateContentItemRequest) (*bumblebeepb.UpdateContentItemResponse, error) {
	item, err := s.itemService.Update(ctx, mapper.ToUpdateInput(mapper.UpdateContentItemFromProto(req)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.UpdateContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) DeleteContentItem(ctx context.Context, req *bumblebeepb.DeleteContentItemRequest) (*bumblebeepb.DeleteContentItemResponse, error) {
	item, err := s.itemService.Delete(ctx, req.GetId())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.DeleteContentItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) CreateContentRevision(ctx context.Context, req *bumblebeepb.CreateContentRevisionRequest) (*bumblebeepb.CreateContentRevisionResponse, error) {
	revision, err := s.itemService.CreateRevision(ctx, mapper.ToCreateRevisionInput(mapper.CreateContentRevisionFromProto(req)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.CreateContentRevisionResponse{Revision: mapper.ToProtoRevision(revision)}, nil
}

func (s *Server) ListContentRevisions(ctx context.Context, req *bumblebeepb.ListContentRevisionsRequest) (*bumblebeepb.ListContentRevisionsResponse, error) {
	revisions, err := s.itemService.ListRevisions(ctx, req.GetContentItemId())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.ListContentRevisionsResponse{Revisions: mapper.ToProtoRevisions(revisions)}, nil
}
