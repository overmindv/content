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
		SELECT published_revision_id,
		       EXISTS (SELECT 1 FROM content_revisions WHERE content_item_id = $1 AND id = $2)
		FROM content_items
		WHERE id = $1 AND status = 'published' AND published_revision_id IS NOT NULL
		FOR SHARE
	`
	var publishedRevisionID string
	var baseExists bool
	err = tx.QueryRow(ctx, getPublishedRevision, input.ContentItemID, input.BaseRevisionID).Scan(&publishedRevisionID, &baseExists)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChangeProposal{}, service.ErrNotFound
	}
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	if publishedRevisionID != input.BaseRevisionID {
		return domain.ChangeProposal{}, service.ErrRevisionConflict
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
	if err = insertProposalRevision(ctx, tx, proposalID, headID, "", 1, input.Source, input.Message); err != nil {
		return domain.ChangeProposal{}, err
	}

	return finishProposalWrite(ctx, tx, proposalID, input.AuthorID)
}

func (s *ContentItemStore) GetChangeProposal(ctx context.Context, id, actorID string) (domain.ChangeProposal, error) {
	return getProposal(ctx, s.db, id, actorID, false)
}

func (s *ContentItemStore) AppendChangeProposalRevision(ctx context.Context, input domain.AppendChangeProposalRevisionInput) (domain.ChangeProposal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const lockProposal = `
		SELECT status, head_revision_id FROM content_change_proposals
		WHERE id = $1 AND author_id = $2
		FOR UPDATE
	`
	var state domain.ChangeProposalStatus
	var headID string
	err = tx.QueryRow(ctx, lockProposal, input.ProposalID, input.AuthorID).Scan(&state, &headID)
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

	nextID := uuid.NewString()
	const appendRevision = `
		INSERT INTO content_change_proposal_revisions (proposal_id, id, parent_revision_id, ordinal, source, message)
		SELECT proposal_id, $2, id, ordinal + 1, $4, $5
		FROM content_change_proposal_revisions
		WHERE proposal_id = $1 AND id = $3
	`
	tag, err := tx.Exec(ctx, appendRevision, input.ProposalID, nextID, headID, input.Source, input.Message)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	if tag.RowsAffected() != 1 {
		return domain.ChangeProposal{}, service.ErrInvalidState
	}
	if _, err = tx.Exec(ctx, `UPDATE content_change_proposals SET head_revision_id = $2 WHERE id = $1`, input.ProposalID, nextID); err != nil {
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
		UPDATE content_change_proposals SET status = 'closed', closed_at = NOW()
		WHERE id = $1 AND author_id = $2 AND status = 'open'
	`
	if _, err = tx.Exec(ctx, closeProposal, id, actorID); err != nil {
		return domain.ChangeProposal{}, err
	}

	return finishProposalWrite(ctx, tx, id, actorID)
}

func insertProposalRevision(ctx context.Context, tx pgx.Tx, proposalID, id, parentID string, ordinal int32, source, message string) error {
	const insertRevision = `
		INSERT INTO content_change_proposal_revisions (proposal_id, id, parent_revision_id, ordinal, source, message)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := tx.Exec(ctx, insertRevision, proposalID, id, nullableString(parentID), ordinal, source, message)

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
		SELECT p.id, p.content_item_id, p.base_revision_id, p.author_id, p.status, p.created_at, p.closed_at,
		       r.id, r.parent_revision_id, r.ordinal, r.source, r.message, r.created_at
		FROM content_change_proposals AS p
		JOIN content_items AS i ON i.id = p.content_item_id
		JOIN content_change_proposal_revisions AS r ON r.proposal_id = p.id AND r.id = p.head_revision_id
		WHERE p.id = $1 AND (NOT $3::boolean OR p.author_id = $2) AND ` + proposalReader
	var proposal domain.ChangeProposal
	var parentID *string
	err := q.QueryRow(ctx, selectProposal, id, actorID, authorOnly).Scan(
		&proposal.ID, &proposal.ContentItemID, &proposal.BaseRevisionID, &proposal.AuthorID, &proposal.Status,
		&proposal.CreatedAt, &proposal.ClosedAt, &proposal.Head.ID, &parentID,
		&proposal.Head.Ordinal, &proposal.Head.Source, &proposal.Head.Message, &proposal.Head.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChangeProposal{}, service.ErrProposalNotFound
	}
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	proposal.Head.ParentRevisionID = stringFromPtr(parentID)

	return proposal, nil
}
