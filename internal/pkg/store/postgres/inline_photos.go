package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

func draftPhotoIDs(format domain.ContentFormat, source string) ([]string, error) {
	if format != domain.ContentFormatMarkdown || !strings.Contains(source, validator.PhotoBlockLanguage) {
		return nil, nil
	}

	return validator.PublishablePhotoIDs(format, source)
}

func requireRevisionPhotos(ctx context.Context, tx pgx.Tx, itemID string, revision attachmentRevision, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	const query = `SELECT count(DISTINCT asset_id)` + mergeAttachmentSource +
		` AND kind = 'image' AND asset_id = ANY ($4::uuid[])`
	var count int
	if err := tx.QueryRow(ctx, query, itemID, revision.revisionID, nullableString(revision.proposalID), ids).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return validator.Error{Message: "inline photos must reference image attachments from the same revision"}
	}

	return nil
}
