package validator

import (
	"errors"
	"strings"

	"github.com/overmindv/bumblebee/internal/pkg/domain"
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

func ValidateCreateTemplateItem(input domain.CreateTemplateItemInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return Error{Message: "name is required"}
	}
	if len(input.Name) > 128 {
		return Error{Message: "name must be shorter than 129 characters"}
	}
	if !isAllowedStatus(input.Status) {
		return Error{Message: "status must be one of: draft, active, archived"}
	}

	return nil
}

func ValidateUpdateTemplateItem(input domain.UpdateTemplateItemInput) error {
	if err := ValidateID(input.ID); err != nil {
		return err
	}
	if strings.TrimSpace(input.Name) == "" {
		return Error{Message: "name is required"}
	}
	if !isAllowedStatus(input.Status) {
		return Error{Message: "status must be one of: draft, active, archived"}
	}

	return nil
}

func isAllowedStatus(status string) bool {
	switch status {
	case "draft", "active", "archived":
		return true
	default:
		return false
	}
}
