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
	Learning    *LearningMetadata
}

type UpdateContentItem struct {
	ID          string
	Type        string
	Status      string
	Title       string
	Slug        string
	Description string
	UpdatedBy   string
	Learning    *LearningMetadata
}

type LearningMetadata struct {
	TopicIDs         []string
	EstimatedMinutes int32
	LearningOutcomes []string
}

type CreateContentRevision struct {
	ContentItemID      string
	ExpectedRevisionID string
	Format             string
	Source             string
	Message            string
	CreatedBy          string
}

type CreateContentAsset struct {
	AssetID  string
	Kind     string
	Title    string
	Position int
}
