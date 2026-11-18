package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/pkg/api/activity"
)

const materialModerationQuery = `
	SELECT published_revision_id, moderation_version, moderation_hidden
	FROM content_items WHERE id = $1 AND status = 'published' AND published_revision_id IS NOT NULL`

func (s *ContentItemStore) GetMaterialModeration(ctx context.Context, id string) (domain.MaterialModeration, error) {
	return readModeration(s.db.QueryRow(ctx, materialModerationQuery, id), id)
}

func readModeration(row scanner, id string) (domain.MaterialModeration, error) {
	value := domain.MaterialModeration{MaterialID: id}
	err := row.Scan(&value.PublishedRevisionID, &value.Version, &value.Hidden)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MaterialModeration{}, service.ErrNotFound
	}
	if err != nil {
		return domain.MaterialModeration{}, err
	}
	return value, nil
}

func (s *ContentItemStore) SetMaterialModeration(ctx context.Context, input domain.SetMaterialModerationInput) (domain.MaterialModeration, error) {
	var value domain.MaterialModeration
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		current, err := readModeration(tx.QueryRow(ctx, materialModerationQuery+` FOR UPDATE`, input.MaterialID), input.MaterialID)
		if err != nil {
			return err
		}
		if current.Version != input.Version || current.PublishedRevisionID != input.PublishedRevisionID {
			return service.ErrModerationConflict
		}
		if current.Hidden == input.Hidden {
			return service.ErrInvalidState
		}

		const update = `
			WITH changed AS (
			    UPDATE content_items SET moderation_hidden = $2, moderation_version = moderation_version + 1
			    WHERE id = $1 RETURNING published_revision_id, moderation_version, moderation_hidden
			)
			INSERT INTO content_moderation_audit (material_id, version, published_revision_id, actor_id, hidden, reason)
			SELECT $1, moderation_version, published_revision_id, $3, moderation_hidden, $4 FROM changed
			RETURNING published_revision_id, version, hidden`
		value, err = readModeration(tx.QueryRow(ctx, update, input.MaterialID, input.Hidden, input.ActorID, input.Reason), input.MaterialID)
		if err != nil {
			return err
		}
		item := domain.ContentItem{ID: input.MaterialID, PublishedRevisionID: value.PublishedRevisionID}
		kind := activity.MaterialWithdrawn
		invalidatedRevisionID := value.PublishedRevisionID
		if !value.Hidden {
			kind = activity.MaterialRestored
			invalidatedRevisionID = ""
			item, err = s.get(ctx, tx, input.MaterialID, "")
			if err != nil {
				return err
			}
		}
		_, err = s.recordMaterialState(ctx, tx, item, kind, invalidatedRevisionID)
		return err
	})
	if err != nil {
		return domain.MaterialModeration{}, err
	}
	return value, nil
}
