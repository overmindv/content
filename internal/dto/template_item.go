package dto

type CreateTemplateItem struct {
	Name        string
	Description string
	Status      string
}

type UpdateTemplateItem struct {
	ID          string
	Name        string
	Description string
	Status      string
}
