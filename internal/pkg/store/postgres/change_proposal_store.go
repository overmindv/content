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
		SELECT i.published_revision_id, r.source = $2
		FROM content_items AS i
		JOIN content_revisions AS r ON r.content_item_id = i.id AND r.id = i.published_revision_id
		WHERE i.id = $1 AND i.status = 'published' AND NOT i.moderation_hidden
		FOR SHARE OF i
	`
	var publishedRevisionID string
	var sameText bool
	err = tx.QueryRow(ctx, getPublishedRevision, input.ContentItemID, input.Source).Scan(&publishedRevisionID, &sameText)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChangeProposal{}, service.ErrNotFound
	}
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	if publishedRevisionID != input.BaseRevisionID {
		return domain.ChangeProposal{}, service.ErrRevisionConflict
	}
	if sameText && input.Attachments == nil {
		return domain.ChangeProposal{}, service.ErrProposalNoChange
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
	if err = insertProposalRevision(ctx, tx, proposalID, headID, "", nil, 1, input.Source, input.Message); err != nil {
		return domain.ChangeProposal{}, err
	}
	if err = s.saveRevisionAttachments(ctx, tx, input.ContentItemID, input.AuthorID,
		attachmentRevision{revisionID: headID, proposalID: proposalID}, attachmentRevision{revisionID: publishedRevisionID}, input.Attachments); err != nil {
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
	if input.BaseRevisionID == "" {
		return domain.ChangeProposal{}, service.ErrMergeConflict
	}

	return s.appendProposalRevision(ctx, input.AppendChangeProposalRevisionInput, input.BaseRevisionID)
}

func (s *ContentItemStore) appendProposalRevision(ctx context.Context, input domain.AppendChangeProposalRevisionInput, newBaseID string) (domain.ChangeProposal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	itemID, err := lockProposalEditMaterial(ctx, tx, input, newBaseID)
	if err != nil {
		return domain.ChangeProposal{}, err
	}

	const lockProposal = `
		SELECT status, head_revision_id, base_revision_id FROM content_change_proposals
		WHERE id = $1 AND author_id = $2
		FOR UPDATE
	`
	var state domain.ChangeProposalStatus
	var headID, baseID string
	err = tx.QueryRow(ctx, lockProposal, input.ProposalID, input.AuthorID).Scan(&state, &headID, &baseID)
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
	const getHead = `
		SELECT ordinal, attachments_snapshot, source = $3, base_revision_id
		FROM content_change_proposal_revisions WHERE proposal_id = $1 AND id = $2`
	var ordinal int32
	var hasSnapshot, sameText bool
	var baseOverride *string
	err = tx.QueryRow(ctx, getHead, input.ProposalID, headID, input.Source).Scan(&ordinal, &hasSnapshot, &sameText, &baseOverride)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChangeProposal{}, service.ErrInvalidState
	}
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	if baseOverride != nil {
		baseID = *baseOverride
	}
	if newBaseID != "" {
		if newBaseID == baseID {
			return domain.ChangeProposal{}, service.ErrInvalidState
		}
		baseID, baseOverride = newBaseID, &newBaseID
	} else if sameText && input.Attachments == nil {
		return domain.ChangeProposal{}, service.ErrProposalNoChange
	}
	if err = retireMergeCandidate(ctx, tx, input.ProposalID); err != nil {
		return domain.ChangeProposal{}, err
	}
	if err = insertProposalRevision(ctx, tx, input.ProposalID, nextID, headID, baseOverride, ordinal+1, input.Source, input.Message); err != nil {
		return domain.ChangeProposal{}, err
	}
	source := attachmentRevision{revisionID: baseID}
	if hasSnapshot {
		source = attachmentRevision{revisionID: headID, proposalID: input.ProposalID}
	}
	if err = s.saveRevisionAttachments(ctx, tx, itemID, input.AuthorID,
		attachmentRevision{revisionID: nextID, proposalID: input.ProposalID}, source, input.Attachments); err != nil {
		return domain.ChangeProposal{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE content_change_proposals SET head_revision_id = $2 WHERE id = $1`, input.ProposalID, nextID); err != nil {
		return domain.ChangeProposal{}, err
	}

	return finishProposalWrite(ctx, tx, input.ProposalID, input.AuthorID)
}

func lockProposalEditMaterial(ctx context.Context, tx pgx.Tx, input domain.AppendChangeProposalRevisionInput, newBaseID string) (string, error) {
	// Материал блокируем раньше предложения, как при удалении материала.
	const selectMaterial = `
		SELECT i.id, i.status, i.published_revision_id, i.moderation_hidden
		FROM content_items AS i
		JOIN content_change_proposals AS p ON p.content_item_id = i.id
		WHERE p.id = $1 AND p.author_id = $2`
	query := selectMaterial + ` FOR KEY SHARE OF i`
	if newBaseID != "" {
		query = selectMaterial + ` FOR SHARE OF i`
	}
	var itemID string
	var status domain.ContentStatus
	var published *string
	var hidden bool
	err := tx.QueryRow(ctx, query, input.ProposalID, input.AuthorID).
		Scan(&itemID, &status, &published, &hidden)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", service.ErrProposalNotFound
	}
	if err != nil {
		return "", err
	}
	if hidden {
		return "", service.ErrProposalNotFound
	}
	if newBaseID != "" {
		if status != domain.ContentStatusPublished {
			return "", service.ErrInvalidState
		}
		if stringFromPtr(published) != newBaseID {
			return "", service.ErrMergeConflict
		}
	}

	return itemID, nil
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
	tag, err := tx.Exec(ctx, closeProposal, id, actorID)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	if tag.RowsAffected() == 1 {
		if err = retireMergeCandidate(ctx, tx, id); err != nil {
			return domain.ChangeProposal{}, err
		}
		if err = s.retireAttachmentBindings(ctx, tx, id, true); err != nil {
			return domain.ChangeProposal{}, err
		}
	}

	return finishProposalWrite(ctx, tx, id, actorID)
}

func insertProposalRevision(ctx context.Context, tx pgx.Tx, proposalID, id, parentID string, baseID *string, ordinal int32, source, message string) error {
	const insertRevision = `
		INSERT INTO content_change_proposal_revisions (proposal_id, id, parent_revision_id, ordinal, source, message, attachments_snapshot, base_revision_id)
		VALUES ($1, $2, $3, $4, $5, $6, TRUE, $7)
	`
	_, err := tx.Exec(ctx, insertRevision, proposalID, id, nullableString(parentID), ordinal, source, message, baseID)

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
		SELECT p.id, p.content_item_id, COALESCE(r.base_revision_id, p.base_revision_id), p.author_id, p.status, p.created_at, p.closed_at, p.merged_revision_id,
		       p.status = 'open' AND (i.status <> 'published' OR i.published_revision_id IS DISTINCT FROM COALESCE(r.base_revision_id, p.base_revision_id)),
		       r.id, r.parent_revision_id, r.ordinal, r.source, r.message, r.created_at,
		       (SELECT jsonb_agg(jsonb_build_object(
		            'ID', a.id, 'ContentItemID', a.content_item_id, 'RevisionID', a.proposal_revision_id,
		            'AssetID', a.asset_id, 'Kind', a.kind, 'Title', a.title, 'Position', a.position, 'CreatedAt', a.created_at
		        ) ORDER BY a.position, a.id)
		        FROM content_assets AS a WHERE a.proposal_id = p.id AND a.proposal_revision_id = r.id)
		FROM content_change_proposals AS p
		JOIN content_items AS i ON i.id = p.content_item_id
		JOIN content_change_proposal_revisions AS r ON r.proposal_id = p.id AND r.id = p.head_revision_id
		WHERE p.id = $1 AND (NOT $3::boolean OR p.author_id = $2) AND ` + proposalReader
	var proposal domain.ChangeProposal
	var parentID, mergedID *string
	err := q.QueryRow(ctx, selectProposal, id, actorID, authorOnly).Scan(
		&proposal.ID, &proposal.ContentItemID, &proposal.BaseRevisionID, &proposal.AuthorID, &proposal.Status,
		&proposal.CreatedAt, &proposal.ClosedAt, &mergedID, &proposal.BaseOutdated, &proposal.Head.ID, &parentID,
		&proposal.Head.Ordinal, &proposal.Head.Source, &proposal.Head.Message, &proposal.Head.CreatedAt, &proposal.Head.Assets,
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
