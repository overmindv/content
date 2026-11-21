package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/pkg/api/activity"
)

func (s *ContentItemStore) RestorePublishedRevision(ctx context.Context, input domain.RestorePublishedRevisionInput) (domain.ContentRevision, error) {
	var revision domain.ContentRevision
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		item, err := lockOwnedContentItem(ctx, tx, input.ContentItemID, input.ActorID)
		if err != nil {
			return err
		}
		if item.status != domain.ContentStatusPublished {
			return service.ErrInvalidState
		}
		if item.publishedRevisionID != input.ExpectedPublishedID {
			return service.ErrRevisionConflict
		}
		if input.SourceRevisionID == item.publishedRevisionID {
			return service.ErrInvalidState
		}

		var wasPublished bool
		const published = `
			SELECT EXISTS (SELECT 1 FROM content_catalog_versions
			               WHERE material_id = $1 AND revision_id = $2 AND NOT tombstone)`
		if err = tx.QueryRow(ctx, published, input.ContentItemID, input.SourceRevisionID).Scan(&wasPublished); err != nil {
			return err
		}
		if !wasPublished {
			return service.ErrPublishedRevisionNotFound
		}
		if err = validatePublication(ctx, tx, input.ContentItemID, input.SourceRevisionID); err != nil {
			return err
		}
		source := attachmentRevision{revisionID: input.SourceRevisionID}
		if err = s.requireAttachmentBindings(ctx, tx, input.ContentItemID, source.revisionID); err != nil {
			return err
		}

		const insert = `
			INSERT INTO content_revisions (id, content_item_id, revision, format, source, source_hash, created_by, restored_from_revision_id)
			SELECT $3, $1, (SELECT max(revision) + 1 FROM content_revisions WHERE content_item_id = $1),
			       format, source, source_hash, $4, id
			FROM content_revisions WHERE content_item_id = $1 AND id = $2
			RETURNING id, content_item_id, revision, format, source, source_hash, message, created_by, created_at, restored_from_revision_id`
		revision, err = scanContentRevision(tx.QueryRow(ctx, insert, input.ContentItemID, input.SourceRevisionID, uuid.NewString(), input.ActorID))
		if err != nil {
			return err
		}
		if err = s.saveRevisionAttachments(ctx, tx, input.ContentItemID, input.ActorID,
			attachmentRevision{revisionID: revision.ID}, source, nil); err != nil {
			return err
		}

		const publish = `
			UPDATE content_items SET published_revision_id = $2,
			    current_revision_id = CASE WHEN current_revision_id = published_revision_id THEN $2 ELSE current_revision_id END,
			    published_at = statement_timestamp(), updated_at = statement_timestamp(), updated_by = $3
			WHERE id = $1`
		if _, err = tx.Exec(ctx, publish, input.ContentItemID, revision.ID, input.ActorID); err != nil {
			return err
		}
		updated, err := s.get(ctx, tx, input.ContentItemID, "")
		if err != nil {
			return err
		}
		if err = s.enqueueContentItem(ctx, tx, eventContentItemUpdated, updated); err != nil {
			return err
		}
		_, err = s.recordMaterialEvent(ctx, tx, updated, activity.MaterialCorrected,
			activity.MaterialState{InvalidatedRevisionID: item.publishedRevisionID})

		return err
	})
	if err != nil {
		return domain.ContentRevision{}, err
	}

	return revision, nil
}
