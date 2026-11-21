package domain

type MaterialModeration struct {
	MaterialID          string
	PublishedRevisionID string
	Version             int64
	Hidden              bool
}

type SetMaterialModerationInput struct {
	MaterialModeration
	ActorID string
	Reason  string
}
