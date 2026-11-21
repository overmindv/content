package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/validator"
)

func WithManagedAttachments() StoreOption {
	return func(store *ContentItemStore) { store.manageAttachments = true }
}

type attachmentRevision struct {
	revisionID string
	proposalID string
}

func (s *ContentItemStore) saveRevisionAttachments(ctx context.Context, tx pgx.Tx, itemID, actorID string, target, source attachmentRevision, set *domain.AttachmentSet) error {
	if set != nil && len(set.Assets) == 0 || set == nil && source.revisionID == "" {
		return nil
	}
	var materialRevision, proposalRevision any
	if target.proposalID == "" {
		materialRevision = target.revisionID
	} else {
		proposalRevision = target.revisionID
	}
	var inherited string
	if set != nil {
		const insert = `
			INSERT INTO content_assets (id, content_item_id, revision_id, proposal_id, proposal_revision_id, asset_id, kind, title, position)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)`
		if err := validator.ValidateAssetCount(len(set.Assets)); err != nil {
			return err
		}
		batch := &pgx.Batch{}
		for _, asset := range set.Assets {
			batch.Queue(insert, itemID, materialRevision, nullableString(target.proposalID), proposalRevision,
				asset.AssetID, asset.Kind, asset.Title, asset.Position)
		}
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return err
		}
	} else {
		const inherit = `
			INSERT INTO content_assets (id, content_item_id, revision_id, proposal_id, proposal_revision_id, asset_id, kind, title, position)
			SELECT gen_random_uuid(), $1, $2, $3, $4, asset_id, kind, title, position
			FROM content_assets WHERE content_item_id = $1
			  AND (($6::uuid IS NULL AND revision_id = $5) OR (proposal_id = $6 AND proposal_revision_id = $5))
			ORDER BY position, id LIMIT $7`
		tag, err := tx.Exec(ctx, inherit, itemID, materialRevision, nullableString(target.proposalID), proposalRevision,
			source.revisionID, nullableString(source.proposalID), validator.MaxAssets+1)
		if err != nil {
			return err
		}
		if err = validator.ValidateAssetCount(int(tag.RowsAffected())); err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		inherited = source.revisionID
	}
	if !s.manageAttachments {
		return nil
	}
	const queue = `
		INSERT INTO content_media_bindings (revision_id, material_id, proposal_id, actor_user_id, inherit_from_revision_id, file_ids)
		SELECT $2, $1, $3, $4, $5, array_agg(DISTINCT asset_id ORDER BY asset_id)
		FROM content_assets WHERE content_item_id = $1
		  AND (($3::uuid IS NULL AND revision_id = $2) OR (proposal_id = $3 AND proposal_revision_id = $2))
		HAVING count(*) > 0`
	_, err := tx.Exec(ctx, queue, itemID, target.revisionID, nullableString(target.proposalID), actorID, nullableString(inherited))

	return err
}

func (s *ContentItemStore) requireAttachmentBindings(ctx context.Context, tx pgx.Tx, itemID, revisionID string) error {
	if !s.manageAttachments {
		return nil
	}
	const query = `
		SELECT NOT EXISTS (
		    SELECT 1 FROM content_assets WHERE content_item_id = $1 AND revision_id = $2
		) OR EXISTS (
		    SELECT 1 FROM content_media_bindings
		    WHERE revision_id = $2 AND material_id = $1 AND desired_version = 1 AND applied_version = 1
		)`
	var ready bool
	if err := tx.QueryRow(ctx, query, itemID, revisionID).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return service.ErrAttachmentsPending
	}

	return nil
}

func (s *ContentItemStore) retireAttachmentBindings(ctx context.Context, tx pgx.Tx, id string, proposal bool) error {
	if !s.manageAttachments {
		return nil
	}
	query := `
		UPDATE content_media_bindings SET desired_version = 2, file_ids = '{}',
		    inherit_from_revision_id = NULL, lease_token = NULL, attempts = 0, next_attempt_at = statement_timestamp()
		WHERE desired_version = 1 AND `
	if proposal {
		query += `proposal_id = $1`
	} else {
		query += `material_id = $1`
	}
	_, err := tx.Exec(ctx, query, id)

	return err
}

func (s *ContentItemStore) ClaimMediaBinding(ctx context.Context) (domain.MediaBinding, bool, error) {
	const query = `
		WITH expired AS (
		    SELECT proposal_id FROM content_proposal_merge_candidates
		    WHERE expires_at <= statement_timestamp()
		    ORDER BY expires_at, proposal_id LIMIT 1 FOR UPDATE SKIP LOCKED
		), removed AS (
		    DELETE FROM content_proposal_merge_candidates AS c USING expired
		    WHERE c.proposal_id = expired.proposal_id RETURNING c.revision_id
		), retired AS (` + retireCandidateBindingSQL + ` RETURNING b.revision_id
		), due AS (
		    SELECT revision_id FROM content_media_bindings
		    WHERE applied_version < desired_version AND next_attempt_at <= statement_timestamp()
		      AND revision_id NOT IN (SELECT revision_id FROM retired)
		    ORDER BY next_attempt_at, revision_id LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		UPDATE content_media_bindings AS b SET lease_token = $1,
		  next_attempt_at = statement_timestamp() + INTERVAL '10 seconds', attempts = LEAST(attempts, 1000000) + 1
		FROM due WHERE b.revision_id = due.revision_id
		RETURNING b.material_id, b.revision_id, b.actor_user_id, b.file_ids::text[], b.inherit_from_revision_id,
		          b.desired_version, b.lease_token, b.attempts`
	var b domain.MediaBinding
	var parent *string
	err := s.db.QueryRow(ctx, query, uuid.NewString()).Scan(&b.MaterialID, &b.RevisionID, &b.ActorID, &b.FileIDs,
		&parent, &b.Version, &b.Token, &b.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MediaBinding{}, false, nil
	}
	if err != nil {
		return domain.MediaBinding{}, false, err
	}
	b.InheritFromRevisionID = stringFromPtr(parent)

	return b, true, nil
}

func (s *ContentItemStore) FinishMediaBinding(ctx context.Context, b domain.MediaBinding, retryAfter time.Duration) error {
	const query = `
		UPDATE content_media_bindings SET lease_token = NULL,
		    applied_version = CASE WHEN $4::integer = 0 THEN desired_version ELSE applied_version END,
		    next_attempt_at = statement_timestamp() + ($4::integer * INTERVAL '1 second')
		WHERE revision_id = $1 AND lease_token = $2 AND desired_version = $3`
	_, err := s.db.Exec(ctx, query, b.RevisionID, b.Token, b.Version, int(retryAfter/time.Second))

	return err
}
