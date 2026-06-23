package domain

import "time"

type TemplateItem struct {
	ID          string
	Name        string
	Description string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateTemplateItemInput struct {
	Name        string
	Description string
	Status      string
}

type UpdateTemplateItemInput struct {
	ID          string
	Name        string
	Description string
	Status      string
}
