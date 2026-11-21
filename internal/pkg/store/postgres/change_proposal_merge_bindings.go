package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/validator"
)

func (s *ContentItemStore) prepareMergeRevision(ctx context.Context, tx pgx.Tx, input domain.MergeChangeProposalInput,
	p proposalMerge, source attachmentRevision) (string, bool, error) {
	const getCandidate = `
		SELECT c.revision_id, c.head_revision_id, c.expires_at <= statement_timestamp(),
		       b.desired_version = 1 AND b.applied_version = 1
		FROM content_proposal_merge_candidates c
		JOIN content_media_bindings b ON b.revision_id = c.revision_id
		WHERE c.proposal_id = $1 FOR UPDATE OF c`
	var revisionID, headID string
	var expired, ready bool
	err := tx.QueryRow(ctx, getCandidate, input.ProposalID).Scan(&revisionID, &headID, &expired, &ready)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return "", false, err
	case !expired && headID == p.headID && s.manageAttachments:
		return revisionID, ready, nil
	default:
		if err = retireMergeCandidate(ctx, tx, input.ProposalID); err != nil {
			return "", false, err
		}
	}
	const listFiles = `
		SELECT count(*), COALESCE(array_agg(DISTINCT asset_id ORDER BY asset_id), '{}')::text[]
		FROM (SELECT asset_id` + mergeAttachmentSource + ` LIMIT $4) a`
	var count int
	var fileIDs []string
	err = tx.QueryRow(ctx, listFiles, p.itemID, source.revisionID, nullableString(source.proposalID), validator.MaxAssets+1).
		Scan(&count, &fileIDs)
	if err != nil {
		return "", false, err
	}
	if err = validator.ValidateAssetCount(count); err != nil {
		return "", false, err
	}
	revisionID = uuid.NewString()
	if len(fileIDs) == 0 {
		return revisionID, true, nil
	}
	if !s.manageAttachments {
		return "", false, service.ErrMediaUnavailable
	}
	const queue = `
		WITH binding AS (
		    INSERT INTO content_media_bindings (revision_id, material_id, proposal_id, actor_user_id, inherit_from_revision_id, file_ids)
		    VALUES ($1, $2, $3, $4, $5, $6::uuid[]) RETURNING revision_id
		)
		INSERT INTO content_proposal_merge_candidates (proposal_id, head_revision_id, revision_id)
		SELECT $3, $7, revision_id FROM binding`
	_, err = tx.Exec(ctx, queue, revisionID, p.itemID, input.ProposalID, input.ActorID, source.revisionID, fileIDs, p.headID)

	return revisionID, false, err
}

const retireCandidateBindingSQL = `
	UPDATE content_media_bindings AS b SET desired_version = 2, file_ids = '{}', inherit_from_revision_id = NULL,
	    lease_token = NULL, attempts = 0, next_attempt_at = statement_timestamp()
	FROM removed WHERE b.revision_id = removed.revision_id AND b.desired_version = 1`

func retireMergeCandidate(ctx context.Context, tx pgx.Tx, proposalID string) error {
	const query = `
		WITH removed AS (
		    DELETE FROM content_proposal_merge_candidates WHERE proposal_id = $1 RETURNING revision_id
		)` + retireCandidateBindingSQL
	_, err := tx.Exec(ctx, query, proposalID)

	return err
}

func promoteMergeBinding(ctx context.Context, tx pgx.Tx, proposalID, revisionID string) error {
	const query = `
		WITH promoted AS (
		    DELETE FROM content_proposal_merge_candidates
		    WHERE proposal_id = $1 AND revision_id = $2 RETURNING revision_id
		)
		UPDATE content_media_bindings AS b SET proposal_id = NULL
		FROM promoted WHERE b.revision_id = promoted.revision_id`
	_, err := tx.Exec(ctx, query, proposalID, revisionID)

	return err
}
