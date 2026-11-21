package domain

import "time"

type ChangeProposalStatus string

const (
	ChangeProposalOpen   ChangeProposalStatus = "open"
	ChangeProposalClosed ChangeProposalStatus = "closed"
)

type ChangeProposal struct {
	ID             string
	ContentItemID  string
	BaseRevisionID string
	BaseOutdated   bool
	AuthorID       string
	Status         ChangeProposalStatus
	Head           ChangeProposalRevision
	CreatedAt      time.Time
	ClosedAt       *time.Time
}

type ChangeProposalRevision struct {
	Assets           []ContentAsset
	ID               string
	ParentRevisionID string
	Ordinal          int32
	Source           string
	Message          string
	CreatedAt        time.Time
}

type OpenChangeProposalInput struct {
	Attachments    *AttachmentSet
	ContentItemID  string
	BaseRevisionID string
	AuthorID       string
	Source         string
	Message        string
}

type AppendChangeProposalRevisionInput struct {
	Attachments    *AttachmentSet
	ProposalID     string
	ExpectedHeadID string
	AuthorID       string
	Source         string
	Message        string
}

type ChangeProposalHistoryInput struct {
	ProposalID    string
	ActorID       string
	BeforeOrdinal int32
	PageSize      int32
}

type ChangeProposalHistoryEntry struct {
	ID               string
	ParentRevisionID string
	BaseRevisionID   string
	Ordinal          int32
	AuthorID         string
	Message          string
	CreatedAt        time.Time
}

type ChangeProposalHistory struct {
	Entries           []ChangeProposalHistoryEntry
	NextBeforeOrdinal int32
}

type ChangeProposalDiffInput struct {
	ProposalID string
	RevisionID string
	ActorID    string
}

type ChangeProposalDiffSources struct {
	BaseRevisionID string
	RevisionID     string
	BaseSource     string
	Source         string
	Oversized      bool
}

type ChangeProposalDiff struct {
	BaseRevisionID string
	RevisionID     string
	UnifiedDiff    string
	Truncated      bool
}
