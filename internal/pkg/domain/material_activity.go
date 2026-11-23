package domain

import (
	"time"

	"github.com/overmindv/content/pkg/api/activity"
)

const (
	MaxMaterialAvailabilityBatch = 100
	MaxMaterialSnapshotPage      = 100
)

type MaterialVersionKey struct {
	MaterialID string
	RevisionID string
}

type MaterialSnapshotInput struct {
	Watermark *int64
	AfterID   string
	PageSize  int32
}

type MaterialVersion struct {
	MaterialID         string
	RevisionID         string
	Version            int64
	State              activity.MaterialState
	ActivityOccurredAt *time.Time
}

type MaterialSnapshot struct {
	Watermark   int64
	Entries     []MaterialVersion
	NextAfterID string
}

type ConfirmMaterialReadInput struct {
	MaterialID string
	RevisionID string
	ActorID    string
}
