package service

import (
	"context"
	"strings"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

type PublishedAttachmentSigner interface {
	SignPublishedAttachment(context.Context, domain.PublishedAttachmentRef) (domain.AttachmentDownload, error)
}

func (s *ContentItemService) SetPublishedAttachmentSigner(signer PublishedAttachmentSigner) {
	s.downloads = signer
}

func (s *ContentItemService) DownloadPublishedAttachment(ctx context.Context, ref domain.PublishedAttachmentRef) (domain.AttachmentDownload, error) {
	for _, id := range []*string{&ref.MaterialID, &ref.RevisionID, &ref.FileID} {
		*id = strings.ToLower(*id)
		if !validProposalID(*id) {
			return domain.AttachmentDownload{}, validator.Error{Message: "attachment IDs must be UUIDs"}
		}
	}
	if err := s.store.CheckPublishedAttachment(ctx, ref); err != nil {
		return domain.AttachmentDownload{}, err
	}
	if s.downloads == nil {
		return domain.AttachmentDownload{}, ErrMediaUnavailable
	}

	return s.downloads.SignPublishedAttachment(ctx, ref)
}
