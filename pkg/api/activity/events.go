package activity

import "time"

const (
	SchemaVersion = 1

	MaterialPublished     = "material.published"
	MaterialArchived      = "material.archived"
	MaterialWithdrawn     = "material.withdrawn"
	MaterialRestored      = "material.restored"
	MaterialCorrected     = "material.corrected"
	ContributionMerged    = "contribution.merged"
	ReviewAccepted        = "review.accepted"
	MaterialReadConfirmed = "material.read_confirmed"
	SelfReported          = "self_reported"

	DefaultMaterialTopic     = "content.material.v1"
	DefaultLearningTopic     = "content.learning.v1"
	DefaultContributionTopic = "content.contribution.v1"
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
	Tombstone             bool              `json:"tombstone"`
	Metadata              *ResourceMetadata `json:"metadata,omitempty"`
	InvalidatedRevisionID string            `json:"invalidated_revision_id,omitempty"`
}

type ReadConfirmation struct {
	UserID   string `json:"user_id"`
	Evidence string `json:"evidence"`
}

type EditorialContribution struct {
	ProposalID         string `json:"proposal_id"`
	ProposalRevisionID string `json:"proposal_revision_id"`
	AuthorID           string `json:"author_id"`
	TopicID            string `json:"topic_id"`
	ReviewerID         string `json:"reviewer_id,omitempty"`
	Decision           string `json:"decision,omitempty"`
}
