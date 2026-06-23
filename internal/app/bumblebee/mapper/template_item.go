package mapper

import (
	"github.com/overmindv/bumblebee/internal/dto"
	bumblebee "github.com/overmindv/bumblebee/internal/pkg/api/bumblebee"
	"github.com/overmindv/bumblebee/internal/pkg/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func CreateTemplateItemFromProto(req *bumblebee.CreateTemplateItemRequest) dto.CreateTemplateItem {
	return dto.CreateTemplateItem{
		Name:        req.GetName(),
		Description: req.GetDescription(),
		Status:      req.GetStatus(),
	}
}

func UpdateTemplateItemFromProto(req *bumblebee.UpdateTemplateItemRequest) dto.UpdateTemplateItem {
	return dto.UpdateTemplateItem{
		ID:          req.GetId(),
		Name:        req.GetName(),
		Description: req.GetDescription(),
		Status:      req.GetStatus(),
	}
}

func ToCreateInput(payload dto.CreateTemplateItem) domain.CreateTemplateItemInput {
	return domain.CreateTemplateItemInput{
		Name:        payload.Name,
		Description: payload.Description,
		Status:      payload.Status,
	}
}

func ToUpdateInput(payload dto.UpdateTemplateItem) domain.UpdateTemplateItemInput {
	return domain.UpdateTemplateItemInput{
		ID:          payload.ID,
		Name:        payload.Name,
		Description: payload.Description,
		Status:      payload.Status,
	}
}

func ToProto(item domain.TemplateItem) *bumblebee.TemplateItem {
	return &bumblebee.TemplateItem{
		Id:          item.ID,
		Name:        item.Name,
		Description: item.Description,
		Status:      item.Status,
		CreatedAt:   timestamppb.New(item.CreatedAt),
		UpdatedAt:   timestamppb.New(item.UpdatedAt),
	}
}

func ToProtoList(items []domain.TemplateItem) []*bumblebee.TemplateItem {
	result := make([]*bumblebee.TemplateItem, 0, len(items))
	for _, item := range items {
		result = append(result, ToProto(item))
	}

	return result
}
