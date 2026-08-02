package mapper

import (
	"time"

	"github.com/overmindv/bumblebee/internal/dto"
	bumblebee "github.com/overmindv/bumblebee/internal/pkg/api/bumblebee"
	"github.com/overmindv/bumblebee/internal/pkg/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func CreateContentItemFromProto(req *bumblebee.CreateContentItemRequest) dto.CreateContentItem {
	assets := make([]dto.CreateContentAsset, 0, len(req.GetAssets()))
	for _, asset := range req.GetAssets() {
		assets = append(assets, dto.CreateContentAsset{
			AssetID:  asset.GetAssetId(),
			Kind:     asset.GetKind(),
			Title:    asset.GetTitle(),
			Position: int(asset.GetPosition()),
		})
	}

	return dto.CreateContentItem{
		Type:        req.GetType(),
		Status:      req.GetStatus(),
		Title:       req.GetTitle(),
		Slug:        req.GetSlug(),
		Description: req.GetDescription(),
		Format:      req.GetFormat(),
		Source:      req.GetSource(),
		Message:     req.GetMessage(),
		CreatedBy:   req.GetCreatedBy(),
		Tags:        req.GetTags(),
		Assets:      assets,
	}
}

func UpdateContentItemFromProto(req *bumblebee.UpdateContentItemRequest) dto.UpdateContentItem {
	return dto.UpdateContentItem{
		ID:          req.GetId(),
		Type:        req.GetType(),
		Status:      req.GetStatus(),
		Title:       req.GetTitle(),
		Slug:        req.GetSlug(),
		Description: req.GetDescription(),
		UpdatedBy:   req.GetUpdatedBy(),
	}
}

func CreateContentRevisionFromProto(req *bumblebee.CreateContentRevisionRequest) dto.CreateContentRevision {
	return dto.CreateContentRevision{
		ContentItemID: req.GetContentItemId(),
		Format:        req.GetFormat(),
		Source:        req.GetSource(),
		Message:       req.GetMessage(),
		CreatedBy:     req.GetCreatedBy(),
	}
}

func ToCreateInput(payload dto.CreateContentItem) domain.CreateContentItemInput {
	assets := make([]domain.CreateContentAssetInput, 0, len(payload.Assets))
	for _, asset := range payload.Assets {
		assets = append(assets, domain.CreateContentAssetInput{
			AssetID:  asset.AssetID,
			Kind:     domain.AssetKind(asset.Kind),
			Title:    asset.Title,
			Position: asset.Position,
		})
	}

	return domain.CreateContentItemInput{
		Type:        domain.ContentType(payload.Type),
		Status:      domain.ContentStatus(payload.Status),
		Title:       payload.Title,
		Slug:        payload.Slug,
		Description: payload.Description,
		Format:      domain.ContentFormat(payload.Format),
		Source:      payload.Source,
		Message:     payload.Message,
		CreatedBy:   payload.CreatedBy,
		Tags:        payload.Tags,
		Assets:      assets,
	}
}

func ToUpdateInput(payload dto.UpdateContentItem) domain.UpdateContentItemInput {
	return domain.UpdateContentItemInput{
		ID:          payload.ID,
		Type:        domain.ContentType(payload.Type),
		Status:      domain.ContentStatus(payload.Status),
		Title:       payload.Title,
		Slug:        payload.Slug,
		Description: payload.Description,
		UpdatedBy:   payload.UpdatedBy,
	}
}

func ToCreateRevisionInput(payload dto.CreateContentRevision) domain.CreateContentRevisionInput {
	return domain.CreateContentRevisionInput{
		ContentItemID: payload.ContentItemID,
		Format:        domain.ContentFormat(payload.Format),
		Source:        payload.Source,
		Message:       payload.Message,
		CreatedBy:     payload.CreatedBy,
	}
}

func ToProto(item domain.ContentItem) *bumblebee.ContentItem {
	result := &bumblebee.ContentItem{
		Id:                  item.ID,
		Type:                string(item.Type),
		Status:              string(item.Status),
		Title:               item.Title,
		Slug:                item.Slug,
		Description:         item.Description,
		CurrentRevisionId:   item.CurrentRevisionID,
		PublishedRevisionId: item.PublishedRevisionID,
		CreatedBy:           item.CreatedBy,
		UpdatedBy:           item.UpdatedBy,
		CreatedAt:           timestamppb.New(item.CreatedAt),
		UpdatedAt:           timestamppb.New(item.UpdatedAt),
		PublishedAt:         timestampOrNil(item.PublishedAt),
		ArchivedAt:          timestampOrNil(item.ArchivedAt),
		Tags:                ToProtoTags(item.Tags),
		Assets:              ToProtoAssets(item.Assets),
	}
	if item.CurrentRevision != nil {
		result.CurrentRevision = ToProtoRevision(*item.CurrentRevision)
	}

	return result
}

func ToProtoList(items []domain.ContentItem) []*bumblebee.ContentItem {
	result := make([]*bumblebee.ContentItem, 0, len(items))
	for _, item := range items {
		result = append(result, ToProto(item))
	}

	return result
}

func ToProtoRevision(revision domain.ContentRevision) *bumblebee.ContentRevision {
	return &bumblebee.ContentRevision{
		Id:            revision.ID,
		ContentItemId: revision.ContentItemID,
		Revision:      int32(revision.Revision),
		Format:        string(revision.Format),
		Source:        revision.Source,
		SourceHash:    revision.SourceHash,
		Message:       revision.Message,
		CreatedBy:     revision.CreatedBy,
		CreatedAt:     timestamppb.New(revision.CreatedAt),
	}
}

func ToProtoRevisions(revisions []domain.ContentRevision) []*bumblebee.ContentRevision {
	result := make([]*bumblebee.ContentRevision, 0, len(revisions))
	for _, revision := range revisions {
		result = append(result, ToProtoRevision(revision))
	}

	return result
}

func ToProtoTags(tags []domain.Tag) []*bumblebee.Tag {
	result := make([]*bumblebee.Tag, 0, len(tags))
	for _, tag := range tags {
		result = append(result, &bumblebee.Tag{
			Id:        tag.ID,
			Name:      tag.Name,
			Slug:      tag.Slug,
			CreatedAt: timestamppb.New(tag.CreatedAt),
		})
	}

	return result
}

func ToProtoAssets(assets []domain.ContentAsset) []*bumblebee.ContentAsset {
	result := make([]*bumblebee.ContentAsset, 0, len(assets))
	for _, asset := range assets {
		result = append(result, &bumblebee.ContentAsset{
			Id:            asset.ID,
			ContentItemId: asset.ContentItemID,
			RevisionId:    asset.RevisionID,
			AssetId:       asset.AssetID,
			Kind:          string(asset.Kind),
			Title:         asset.Title,
			Position:      int32(asset.Position),
			CreatedAt:     timestamppb.New(asset.CreatedAt),
		})
	}

	return result
}

func timestampOrNil(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}

	return timestamppb.New(*value)
}
