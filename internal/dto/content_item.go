package dto

type CreateContentItem struct {
	Type        string
	Status      string
	Title       string
	Slug        string
	Description string
	Format      string
	Source      string
	Message     string
	CreatedBy   string
	Tags        []string
	Assets      []CreateContentAsset
}

type UpdateContentItem struct {
	ID          string
	Type        string
	Status      string
	Title       string
	Slug        string
	Description string
	UpdatedBy   string
}

type CreateContentRevision struct {
	ContentItemID string
	Format        string
	Source        string
	Message       string
	CreatedBy     string
}

type CreateContentAsset struct {
	AssetID  string
	Kind     string
	Title    string
	Position int
}
