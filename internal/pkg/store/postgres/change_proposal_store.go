package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

func (s *ContentItemStore) OpenChangeProposal(ctx context.Context, input domain.OpenChangeProposalInput) (domain.ChangeProposal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const getPublishedRevision = `
		SELECT published_revision_id FROM content_items
		WHERE id = $1 AND status = 'published' AND published_revision_id IS NOT NULL
		FOR SHARE
	`
	var publishedRevisionID string
	err = tx.QueryRow(ctx, getPublishedRevision, input.ContentItemID).Scan(&publishedRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChangeProposal{}, service.ErrNotFound
	}
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	if publishedRevisionID != input.BaseRevisionID {
		return domain.ChangeProposal{}, service.ErrRevisionConflict
	}
	const checkBase = `SELECT EXISTS (SELECT 1 FROM content_revisions WHERE content_item_id = $1 AND id = $2)`
	var baseExists bool
	if err = tx.QueryRow(ctx, checkBase, input.ContentItemID, publishedRevisionID).Scan(&baseExists); err != nil {
		return domain.ChangeProposal{}, err
	}
	if !baseExists {
		return domain.ChangeProposal{}, service.ErrNotFound
	}

	proposalID, headID := uuid.NewString(), uuid.NewString()
	const insertProposal = `
		INSERT INTO content_change_proposals (id, content_item_id, base_revision_id, head_revision_id, author_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (content_item_id, author_id) WHERE status = 'open' DO NOTHING
	`
	tag, err := tx.Exec(ctx, insertProposal, proposalID, input.ContentItemID, publishedRevisionID, headID, input.AuthorID)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.ChangeProposal{}, service.ErrProposalExists
	}
	if err = insertProposalRevision(ctx, tx, proposalID, headID, "", "", 1, input.Source, input.Message); err != nil {
		return domain.ChangeProposal{}, err
	}
	return finishProposalWrite(ctx, tx, proposalID, input.AuthorID)
}

func (s *ContentItemStore) GetChangeProposal(ctx context.Context, id, actorID string) (domain.ChangeProposal, error) {
	return getProposal(ctx, s.db, id, actorID, false)
}

func (s *ContentItemStore) AppendChangeProposalRevision(ctx context.Context, input domain.AppendChangeProposalRevisionInput) (domain.ChangeProposal, error) {
	return s.appendProposalRevision(ctx, input, "")
}

func (s *ContentItemStore) RebaseChangeProposal(ctx context.Context, input domain.RebaseChangeProposalInput) (domain.ChangeProposal, error) {
	return s.appendProposalRevision(ctx, input.AppendChangeProposalRevisionInput, input.BaseRevisionID)
}

func (s *ContentItemStore) appendProposalRevision(ctx context.Context, input domain.AppendChangeProposalRevisionInput, newBaseID string) (domain.ChangeProposal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if newBaseID != "" {
		material, err := lockProposalMaterial(ctx, tx, input.ProposalID, input.AuthorID, true)
		if err != nil {
			return domain.ChangeProposal{}, err
		}
		if material.status != domain.ContentStatusPublished {
			return domain.ChangeProposal{}, service.ErrInvalidState
		}
		if material.publishedRevisionID != newBaseID {
			return domain.ChangeProposal{}, service.ErrMergeConflict
		}
	}

	const lockProposal = `
		SELECT status, head_revision_id, base_revision_id FROM content_change_proposals
		WHERE id = $1 AND author_id = $2
		FOR UPDATE
	`
	var state domain.ChangeProposalStatus
	var headID, originalBaseID string
	err = tx.QueryRow(ctx, lockProposal, input.ProposalID, input.AuthorID).Scan(&state, &headID, &originalBaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChangeProposal{}, service.ErrProposalNotFound
	}
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	if state != domain.ChangeProposalOpen {
		return domain.ChangeProposal{}, service.ErrInvalidState
	}
	if headID != input.ExpectedHeadID {
		return domain.ChangeProposal{}, service.ErrRevisionConflict
	}

	const getRevision = `SELECT ordinal, base_revision_id FROM content_change_proposal_revisions WHERE proposal_id = $1 AND id = $2`
	var ordinal int32
	var baseOverride *string
	if err = tx.QueryRow(ctx, getRevision, input.ProposalID, headID).Scan(&ordinal, &baseOverride); err != nil {
		return domain.ChangeProposal{}, err
	}
	baseID := stringFromPtr(baseOverride)
	if newBaseID != "" {
		if newBaseID == baseID || (baseID == "" && newBaseID == originalBaseID) {
			return domain.ChangeProposal{}, service.ErrInvalidState
		}
		baseID = newBaseID
	}
	nextID := uuid.NewString()
	if err = insertProposalRevision(ctx, tx, input.ProposalID, nextID, headID, baseID, ordinal+1, input.Source, input.Message); err != nil {
		return domain.ChangeProposal{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE content_change_proposals SET head_revision_id = $2 WHERE id = $1`, input.ProposalID, nextID); err != nil {
		return domain.ChangeProposal{}, err
	}
	if err = cancelReviewCheck(ctx, tx, input.ProposalID, headID); err != nil {
		return domain.ChangeProposal{}, err
	}
	return finishProposalWrite(ctx, tx, input.ProposalID, input.AuthorID)
}

func (s *ContentItemStore) CloseChangeProposal(ctx context.Context, id, actorID string) (domain.ChangeProposal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const closeProposal = `
		WITH closed AS (
		    UPDATE content_change_proposals SET status = 'closed', closed_at = NOW()
		    WHERE id = $1 AND author_id = $2 AND status = 'open'
		    RETURNING id, head_revision_id
		)
		UPDATE content_change_proposal_revisions AS r
		SET review_due_at = NULL, review_check_token = NULL
		FROM closed WHERE r.proposal_id = closed.id AND r.id = closed.head_revision_id
	`
	if _, err = tx.Exec(ctx, closeProposal, id, actorID); err != nil {
		return domain.ChangeProposal{}, err
	}
	return finishProposalWrite(ctx, tx, id, actorID)
}

func insertProposalRevision(ctx context.Context, tx pgx.Tx, proposalID, id, parentID, baseID string, ordinal int32, source, message string) error {
	const insertRevision = `
		INSERT INTO content_change_proposal_revisions (proposal_id, id, parent_revision_id, ordinal, source, message, base_revision_id, review_due_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`
	_, err := tx.Exec(ctx, insertRevision, proposalID, id, nullableString(parentID), ordinal, source, message, nullableString(baseID))
	return err
}

func cancelReviewCheck(ctx context.Context, tx pgx.Tx, proposalID, revisionID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE content_change_proposal_revisions SET review_due_at = NULL, review_check_token = NULL
		WHERE proposal_id = $1 AND id = $2
	`, proposalID, revisionID)
	return err
}

func finishProposalWrite(ctx context.Context, tx pgx.Tx, id, actorID string) (domain.ChangeProposal, error) {
	proposal, err := getProposal(ctx, tx, id, actorID, true)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ChangeProposal{}, err
	}
	return proposal, nil
}

func getProposal(ctx context.Context, q queryer, id, actorID string, authorOnly bool) (domain.ChangeProposal, error) {
	const selectProposal = `
		SELECT p.id, p.content_item_id, COALESCE(r.base_revision_id, p.base_revision_id),
		       p.author_id, p.status, p.created_at, p.closed_at, p.merged_revision_id,
		       p.status = 'open' AND i.published_revision_id IS DISTINCT FROM COALESCE(r.base_revision_id, p.base_revision_id),
		       r.id, r.parent_revision_id, r.ordinal, r.source, r.message, r.created_at
		FROM content_change_proposals AS p
		JOIN content_items AS i ON i.id = p.content_item_id
		JOIN content_change_proposal_revisions AS r ON r.proposal_id = p.id AND r.id = p.head_revision_id
		WHERE p.id = $1 AND (NOT $3::boolean OR p.author_id = $2) AND ` + proposalReader
	var proposal domain.ChangeProposal
	var parentID, mergedID *string
	err := q.QueryRow(ctx, selectProposal, id, actorID, authorOnly).Scan(
		&proposal.ID, &proposal.ContentItemID, &proposal.BaseRevisionID, &proposal.AuthorID, &proposal.Status,
		&proposal.CreatedAt, &proposal.ClosedAt, &mergedID, &proposal.BaseOutdated, &proposal.Head.ID, &parentID,
		&proposal.Head.Ordinal, &proposal.Head.Source, &proposal.Head.Message, &proposal.Head.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChangeProposal{}, service.ErrProposalNotFound
	}
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	proposal.Head.ParentRevisionID = stringFromPtr(parentID)
	proposal.MergedRevisionID = stringFromPtr(mergedID)
	return proposal, nil
}
