package validator

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
)

type Error struct {
	Message string
}

func (e Error) Error() string {
	return e.Message
}

func IsValidationError(err error) bool {
	var validationErr Error
	return errors.As(err, &validationErr)
}

func ValidateID(id string) error {
	if strings.TrimSpace(id) == "" {
		return Error{Message: "id is required"}
	}

	return nil
}

func ValidateCreateContentItem(input domain.CreateContentItemInput) error {
	if !isAllowedContentType(input.Type) {
		return Error{Message: "type must be one of: article, note, summary, theory"}
	}
	if input.Status != domain.ContentStatusDraft {
		return Error{Message: "new content must have draft status"}
	}
	if err := ValidateContentTitle(input.Title); err != nil {
		return err
	}
	if !isSlug(input.Slug) {
		return Error{Message: "slug must contain lowercase latin letters, digits and single hyphens"}
	}
	if !isAllowedContentFormat(input.Format) {
		return Error{Message: "format must be one of: markdown, typst"}
	}
	if err := validateSource(input.Format, input.Source); err != nil {
		return err
	}
	if err := ValidateAssetCount(len(input.Assets)); err != nil {
		return err
	}
	for _, tag := range input.Tags {
		if strings.TrimSpace(tag) == "" {
			return Error{Message: "tag must not be empty"}
		}
	}
	for _, asset := range input.Assets {
		if strings.TrimSpace(asset.AssetID) == "" {
			return Error{Message: "asset_id is required"}
		}
		if !isAllowedAssetKind(asset.Kind) {
			return Error{Message: "asset kind must be one of: image, attachment, pdf, archive"}
		}
	}

	return nil
}

func ValidateContentTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return Error{Message: "title is required"}
	}
	if len(title) > 256 || !utf8.ValidString(title) || strings.IndexByte(title, 0) >= 0 {
		return Error{Message: "title must be UTF-8 text without NUL, up to 256 bytes"}
	}
	return nil
}

func ValidateUpdateContentItem(input domain.UpdateContentItemInput) error {
	if err := ValidateID(input.ID); err != nil {
		return err
	}
	if !isAllowedContentType(input.Type) {
		return Error{Message: "type must be one of: article, note, summary, theory"}
	}
	if input.Status != domain.ContentStatusDraft {
		return Error{Message: "use the publish or archive operation to change status"}
	}
	if strings.TrimSpace(input.Title) == "" {
		return Error{Message: "title is required"}
	}
	if !isSlug(input.Slug) {
		return Error{Message: "slug must contain lowercase latin letters, digits and single hyphens"}
	}

	return nil
}

func ValidateCreateContentRevision(input domain.CreateContentRevisionInput) error {
	if err := ValidateID(input.ContentItemID); err != nil {
		return err
	}
	if err := ValidateExpectedRevisionID(input.ExpectedRevisionID); err != nil {
		return err
	}
	if !isAllowedContentFormat(input.Format) {
		return Error{Message: "format must be one of: markdown, typst"}
	}
	return validateSource(input.Format, input.Source)
}

func ValidateExpectedRevisionID(value string) error {
	id, err := uuid.Parse(value)
	if len(value) != 36 || err != nil || id == uuid.Nil {
		return Error{Message: "expected_revision_id must be a UUID"}
	}
	return nil
}

func isAllowedContentType(contentType domain.ContentType) bool {
	switch contentType {
	case domain.ContentTypeArticle, domain.ContentTypeNote, domain.ContentTypeSummary, domain.ContentTypeTheory:
		return true
	default:
		return false
	}
}

func isAllowedContentFormat(format domain.ContentFormat) bool {
	switch format {
	case domain.ContentFormatMarkdown, domain.ContentFormatTypst:
		return true
	default:
		return false
	}
}

func isAllowedAssetKind(kind domain.AssetKind) bool {
	switch kind {
	case domain.AssetKindImage, domain.AssetKindAttachment, domain.AssetKindPDF, domain.AssetKindArchive:
		return true
	default:
		return false
	}
}

func isSlug(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	if value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}

	previousHyphen := false
	for _, char := range value {
		isAlpha := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'
		if isAlpha || isDigit {
			previousHyphen = false
			continue
		}
		if char == '-' && !previousHyphen {
			previousHyphen = true
			continue
		}

		return false
	}

	return true
}
