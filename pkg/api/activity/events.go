package activity

import "time"

const (
	SchemaVersion = 1

	MaterialPublished     = "material.published"
	MaterialArchived      = "material.archived"
	MaterialReadConfirmed = "material.read_confirmed"
	SelfReported          = "self_reported"

	DefaultMaterialTopic = "content.material.v1"
	DefaultLearningTopic = "content.learning.v1"
)

type Event[T any] struct {
	EventID          string    `json:"event_id"`
	EventType        string    `json:"event_type"`
	SchemaVersion    int       `json:"schema_version"`
	OccurredAt       time.Time `json:"occurred_at"`
	AggregateID      string    `json:"aggregate_id"`
	AggregateVersion int64     `json:"aggregate_version"`
	RevisionID       string    `json:"revision_id"`
	Payload          T         `json:"payload"`
}

type ResourceMetadata struct {
	Title            string   `json:"title"`
	Type             string   `json:"type"`
	Format           string   `json:"format"`
	TopicIDs         []string `json:"topic_ids"`
	EstimatedMinutes int32    `json:"estimated_minutes"`
}

type MaterialState struct {
	Tombstone bool              `json:"tombstone"`
	Metadata  *ResourceMetadata `json:"metadata,omitempty"`
}

type ReadConfirmation struct {
	UserID   string `json:"user_id"`
	Evidence string `json:"evidence"`
}
