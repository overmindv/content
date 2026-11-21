package domain

import "github.com/overmindv/content/pkg/api/activity"

type MaterialSnapshotInput struct {
	Watermark *int64
	AfterID   string
	PageSize  int32
}

type MaterialVersion struct {
	MaterialID string
	RevisionID string
	Version    int64
	State      activity.MaterialState
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
