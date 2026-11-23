package content

import (
	"context"

	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) SnapshotMaterials(ctx context.Context, req *contentpb.SnapshotMaterialsRequest) (*contentpb.SnapshotMaterialsResponse, error) {
	var watermark *int64
	if req != nil {
		watermark = req.Watermark
	}
	page, err := s.itemService.SnapshotMaterials(ctx, domain.MaterialSnapshotInput{
		Watermark: watermark, AfterID: req.GetAfterId(), PageSize: req.GetPageSize(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	response := &contentpb.SnapshotMaterialsResponse{Watermark: page.Watermark, NextAfterId: page.NextAfterID}
	response.Entries = make([]*contentpb.MaterialSnapshotEntry, 0, len(page.Entries))
	for _, e := range page.Entries {
		row := &contentpb.MaterialSnapshotEntry{MaterialId: e.MaterialID, RevisionId: e.RevisionID, Version: e.Version, Tombstone: e.State.Tombstone}
		if e.ActivityOccurredAt != nil {
			row.ActivityOccurredAt = timestamppb.New(*e.ActivityOccurredAt)
		}
		if m := e.State.Metadata; m != nil {
			row.Metadata = &contentpb.MaterialSnapshotMetadata{
				Title: m.Title, Type: m.Type, Format: m.Format, TopicIds: m.TopicIDs, EstimatedMinutes: m.EstimatedMinutes,
			}
		}
		response.Entries = append(response.Entries, row)
	}

	return response, nil
}

func (s *Server) ConfirmMaterialRead(ctx context.Context, req *contentpb.ConfirmMaterialReadRequest) (*contentpb.ConfirmMaterialReadResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	err = s.itemService.ConfirmMaterialRead(ctx, domain.ConfirmMaterialReadInput{
		MaterialID: req.GetMaterialId(), RevisionID: req.GetRevisionId(), ActorID: actor,
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.ConfirmMaterialReadResponse{}, nil
}
