package domain

import "time"

type PublishedAttachmentRef struct {
	MaterialID string
	RevisionID string
	FileID     string
}

type AttachmentDownload struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
