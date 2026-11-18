package domain

type AttachmentSet struct {
	Assets []CreateContentAssetInput
}

type MediaBinding struct {
	MaterialID            string
	RevisionID            string
	ActorID               string
	FileIDs               []string
	InheritFromRevisionID string
	Version               int16
	Token                 string
	Attempts              int
}
