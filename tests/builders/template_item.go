package builders

import "github.com/overmindv/bumblebee/internal/pkg/domain"

func TemplateItem() domain.CreateTemplateItemInput {
	return domain.CreateTemplateItemInput{
		Name:        "builder-item",
		Description: "bumblebee component test fixture",
		Status:      "draft",
	}
}
