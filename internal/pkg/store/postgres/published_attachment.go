package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

func (s *ContentItemStore) CheckPublishedAttachment(ctx context.Context, ref domain.PublishedAttachmentRef) error {
	const query = `
		SELECT 1 FROM content_items i
		JOIN content_revisions r ON r.id = i.published_revision_id AND r.content_item_id = i.id
		JOIN content_assets a ON a.content_item_id = i.id AND a.revision_id = r.id
		WHERE i.id = $1 AND i.status = 'published' AND r.id = $2 AND a.asset_id = $3
		LIMIT 1`
	var match int
	err := s.db.QueryRow(ctx, query, ref.MaterialID, ref.RevisionID, ref.FileID).Scan(&match)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return service.ErrNotFound
	case err != nil:
		return fmt.Errorf("find attachment in the published revision: %w", err)
	default:
		return nil
	}
}
