package domain

import "time"

type LearningMetadata struct {
	TopicIDs         []string
	EstimatedMinutes int32
	LearningOutcomes []string
}

type CatalogEntry struct {
	ID         string
	Title      string
	Slug       string
	Status     ContentStatus
	AuthorID   string
	CreatedAt  time.Time
	RevisionID string
	Learning   LearningMetadata
}

type CatalogFilter struct {
	ActorID  string
	TopicID  string
	AuthorID string
	Status   ContentStatus
	PageSize int32
}

type CatalogCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}
