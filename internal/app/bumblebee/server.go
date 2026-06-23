package bumblebee

import (
	"context"

	"github.com/overmindv/bumblebee/internal/app/bumblebee/mapper"
	bumblebeepb "github.com/overmindv/bumblebee/internal/pkg/api/bumblebee"
	"github.com/overmindv/bumblebee/internal/pkg/service"
)

type Server struct {
	bumblebeepb.UnimplementedBumblebeeServiceServer
	itemService *service.TemplateItemService
}

func NewServer(itemService *service.TemplateItemService) *Server {
	return &Server{itemService: itemService}
}

func (s *Server) CreateTemplateItem(ctx context.Context, req *bumblebeepb.CreateTemplateItemRequest) (*bumblebeepb.CreateTemplateItemResponse, error) {
	item, err := s.itemService.Create(ctx, mapper.ToCreateInput(mapper.CreateTemplateItemFromProto(req)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.CreateTemplateItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) GetTemplateItem(ctx context.Context, req *bumblebeepb.GetTemplateItemRequest) (*bumblebeepb.GetTemplateItemResponse, error) {
	item, err := s.itemService.Get(ctx, req.GetId())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.GetTemplateItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) ListTemplateItems(ctx context.Context, _ *bumblebeepb.ListTemplateItemsRequest) (*bumblebeepb.ListTemplateItemsResponse, error) {
	items, err := s.itemService.List(ctx)
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.ListTemplateItemsResponse{Items: mapper.ToProtoList(items)}, nil
}

func (s *Server) UpdateTemplateItem(ctx context.Context, req *bumblebeepb.UpdateTemplateItemRequest) (*bumblebeepb.UpdateTemplateItemResponse, error) {
	item, err := s.itemService.Update(ctx, mapper.ToUpdateInput(mapper.UpdateTemplateItemFromProto(req)))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.UpdateTemplateItemResponse{Item: mapper.ToProto(item)}, nil
}

func (s *Server) DeleteTemplateItem(ctx context.Context, req *bumblebeepb.DeleteTemplateItemRequest) (*bumblebeepb.DeleteTemplateItemResponse, error) {
	item, err := s.itemService.Delete(ctx, req.GetId())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &bumblebeepb.DeleteTemplateItemResponse{Item: mapper.ToProto(item)}, nil
}
