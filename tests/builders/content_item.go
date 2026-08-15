package builders

import "github.com/overmindv/content/internal/pkg/domain"

func ContentItem() domain.CreateContentItemInput {
	return domain.CreateContentItemInput{
		Type:        domain.ContentTypeArticle,
		Status:      domain.ContentStatusDraft,
		Title:       "Builder content item",
		Slug:        "builder-content-item",
		Description: "content component test fixture",
		Format:      domain.ContentFormatMarkdown,
		Source:      "# Builder content item",
		Message:     "initial test revision",
		Tags:        []string{"go", "content"},
	}
}
