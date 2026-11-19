package content

import (
	"context"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type materialCatalogServer struct {
	contentpb.UnimplementedMaterialCatalogServiceServer
	items *service.ContentItemService
}

func (s *materialCatalogServer) CheckMaterialAvailability(ctx context.Context, req *contentpb.CheckMaterialAvailabilityRequest) (*contentpb.CheckMaterialAvailabilityResponse, error) {
	resources := req.GetResources()
	if len(resources) > domain.MaxMaterialAvailabilityBatch {
		return nil, status.Error(codes.InvalidArgument, "provide 1-100 material versions")
	}
	keys := make([]domain.MaterialVersionKey, len(resources))
	for i, key := range resources {
		keys[i] = domain.MaterialVersionKey{MaterialID: key.GetMaterialId(), RevisionID: key.GetRevisionId()}
	}
	available, err := s.items.CheckMaterialAvailability(ctx, keys)
	if err != nil {
		return nil, toStatusError(err)
	}
	items := make([]*contentpb.MaterialVersionKey, 0, len(available))
	for _, key := range available {
		items = append(items, &contentpb.MaterialVersionKey{MaterialId: key.MaterialID, RevisionId: key.RevisionID})
	}
	return &contentpb.CheckMaterialAvailabilityResponse{Resources: items}, nil
}
