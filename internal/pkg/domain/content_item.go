package domain

import "time"

type ContentType string

const (
	ContentTypeArticle ContentType = "article"
	ContentTypeNote    ContentType = "note"
	ContentTypeSummary ContentType = "summary"
	ContentTypeTheory  ContentType = "theory"
)

type ContentStatus string

const (
	ContentStatusDraft     ContentStatus = "draft"
	ContentStatusPublished ContentStatus = "published"
	ContentStatusArchived  ContentStatus = "archived"
)

type ContentFormat string

const (
	ContentFormatMarkdown ContentFormat = "markdown"
	ContentFormatTypst    ContentFormat = "typst"
)

type AssetKind string

const (
	AssetKindImage      AssetKind = "image"
	AssetKindAttachment AssetKind = "attachment"
	AssetKindPDF        AssetKind = "pdf"
	AssetKindArchive    AssetKind = "archive"
)

type ContentItem struct {
	ID                  string
	Type                ContentType
	Status              ContentStatus
	Title               string
	Slug                string
	Description         string
	CurrentRevisionID   string
	PublishedRevisionID string
	CreatedBy           string
	UpdatedBy           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	PublishedAt         *time.Time
	ArchivedAt          *time.Time
	CurrentRevision     *ContentRevision
	Tags                []Tag
	Assets              []ContentAsset
	Learning            *LearningMetadata `json:",omitempty"`
}

type ContentRevision struct {
	ID                     string
	ContentItemID          string
	Revision               int
	Format                 ContentFormat
	Source                 string
	SourceHash             string
	Message                string
	CreatedBy              string
	CreatedAt              time.Time
	RestoredFromRevisionID string `json:",omitempty"`
}

type RevisionHistoryInput struct {
	ContentItemID  string
	ActorID        string
	PageSize       int32
	BeforeRevision int32
}

type Tag struct {
	ID        string
	Name      string
	Slug      string
	CreatedAt time.Time
}

type ContentAsset struct {
	ID            string
	ContentItemID string
	RevisionID    string
	AssetID       string
	Kind          AssetKind
	Title         string
	Position      int
	CreatedAt     time.Time
}

type CreateContentItemInput struct {
	Type        ContentType
	Status      ContentStatus
	Title       string
	Slug        string
	Description string
	Format      ContentFormat
	Source      string
	Message     string
	CreatedBy   string
	Tags        []string
	Assets      []CreateContentAssetInput
	Learning    *LearningMetadata
}

type UpdateContentItemInput struct {
	ID          string
	Type        ContentType
	Status      ContentStatus
	Title       string
	Slug        string
	Description string
	UpdatedBy   string
	Learning    *LearningMetadata
}

type CreateContentRevisionInput struct {
	ContentItemID      string
	ExpectedRevisionID string
	Format             ContentFormat
	Source             string
	Message            string
	CreatedBy          string
	Attachments        *AttachmentSet
}

type ChangeContentItemStateInput struct {
	ID                 string
	ExpectedRevisionID string
	ActorID            string
}

type RestorePublishedRevisionInput struct {
	ContentItemID       string
	ExpectedPublishedID string
	SourceRevisionID    string
	ActorID             string
}

type CreateContentAssetInput struct {
	AssetID  string
	Kind     AssetKind
	Title    string
	Position int
}
