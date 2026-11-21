package content

import (
	"context"

	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) GetMaterialModeration(ctx context.Context, req *contentpb.GetMaterialModerationRequest) (*contentpb.GetMaterialModerationResponse, error) {
	if _, err := actorID(ctx); err != nil {
		return nil, err
	}
	value, err := s.itemService.GetMaterialModeration(ctx, req.GetMaterialId())
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.GetMaterialModerationResponse{State: toModerationState(value)}, nil
}

func (s *Server) SetMaterialModeration(ctx context.Context, req *contentpb.SetMaterialModerationRequest) (*contentpb.SetMaterialModerationResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	var hide bool
	switch req.GetAction() {
	case contentpb.MaterialModerationAction_MATERIAL_MODERATION_ACTION_HIDE:
		hide = true
	case contentpb.MaterialModerationAction_MATERIAL_MODERATION_ACTION_RESTORE:
	default:
		return nil, status.Error(codes.InvalidArgument, "moderation action must be hide or restore")
	}
	if req.ExpectedModerationVersion == nil {
		return nil, status.Error(codes.InvalidArgument, "expected_moderation_version is required")
	}
	value, err := s.itemService.SetMaterialModeration(ctx, domain.SetMaterialModerationInput{
		ActorID: actor, Reason: req.GetReason(),
		MaterialModeration: domain.MaterialModeration{
			MaterialID: req.GetMaterialId(), PublishedRevisionID: req.GetExpectedPublishedRevisionId(),
			Version: req.GetExpectedModerationVersion(), Hidden: hide,
		},
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.SetMaterialModerationResponse{State: toModerationState(value)}, nil
}

func toModerationState(value domain.MaterialModeration) *contentpb.MaterialModerationState {
	return &contentpb.MaterialModerationState{
		MaterialId: value.MaterialID, PublishedRevisionId: value.PublishedRevisionID,
		Version: value.Version, Hidden: value.Hidden,
	}
}
