package content

import (
	"context"

	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) GetPublishedAttachmentDownload(ctx context.Context, req *contentpb.GetPublishedAttachmentDownloadRequest) (*contentpb.GetPublishedAttachmentDownloadResponse, error) {
	if _, err := actorID(ctx); err != nil {
		return nil, err
	}
	download, err := s.itemService.DownloadPublishedAttachment(ctx, domain.PublishedAttachmentRef{
		MaterialID: req.GetMaterialId(), RevisionID: req.GetRevisionId(), FileID: req.GetFileId(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.GetPublishedAttachmentDownloadResponse{
		Url: download.URL, ExpiresAt: timestamppb.New(download.ExpiresAt),
	}, nil
}
