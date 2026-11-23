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

const currentInitialDraft = `
	i.status = 'draft' AND i.published_revision_id IS NULL
	AND i.current_revision_id IS NOT DISTINCT FROM p.base_revision_id AND i.created_by = p.author_id`

const activeReviewTarget = `
	NOT i.moderation_hidden AND (
	    (NOT p.initial_publication AND i.status = 'published') OR
	    (p.initial_publication AND ` + currentInitialDraft + `)
	)`

func (s *ContentItemStore) SubmitInitialPublication(ctx context.Context, input domain.SubmitInitialPublicationInput) (domain.ChangeProposal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	item, err := lockOwnedContentItem(ctx, tx, input.ContentItemID, input.AuthorID)
	if err != nil {
		return domain.ChangeProposal{}, err
	}
	if item.hidden {
		return domain.ChangeProposal{}, service.ErrInvalidState
	}
	if item.publishedRevisionID != "" {
		return replayInitialPublication(ctx, tx, input)
	}
	if item.status != domain.ContentStatusDraft {
		return domain.ChangeProposal{}, service.ErrInvalidState
	}
	if err = item.checkRevision(input.ExpectedRevisionID); err != nil {
		return domain.ChangeProposal{}, err
	}
	var openID, baseID string
	var initial bool
	err = tx.QueryRow(ctx, `
		SELECT id, base_revision_id, initial_publication FROM content_change_proposals
		WHERE content_item_id = $1 AND author_id = $2 AND status = 'open'
		FOR UPDATE`, input.ContentItemID, input.AuthorID).Scan(&openID, &baseID, &initial)
	switch {
	case err == nil && !initial:
		return domain.ChangeProposal{}, service.ErrProposalExists
	case err == nil && baseID == item.revisionID:
		return finishProposalWrite(ctx, tx, openID, input.AuthorID)
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return domain.ChangeProposal{}, err
	}
	if err = validatePublication(ctx, tx, input.ContentItemID, item.revisionID); err != nil {
		return domain.ChangeProposal{}, err
	}
	if err = s.requireInitialDraftBindings(ctx, tx, input.ContentItemID, item.revisionID); err != nil {
		return domain.ChangeProposal{}, err
	}
	var source string
	var hasTopic bool
	const draftForReview = `
		SELECT source, EXISTS (SELECT 1 FROM content_item_topics WHERE content_item_id = $1)
		FROM content_revisions WHERE content_item_id = $1 AND id = $2`
	if err = tx.QueryRow(ctx, draftForReview, input.ContentItemID, item.revisionID).Scan(&source, &hasTopic); err != nil {
		return domain.ChangeProposal{}, err
	}
	if !hasTopic {
		return domain.ChangeProposal{}, validator.Error{Message: "a topic is required for initial publication review"}
	}
	if openID != "" {
		if err = s.invalidateInitialPublication(ctx, tx, input.ContentItemID, input.AuthorID); err != nil {
			return domain.ChangeProposal{}, err
		}
	}

	proposalID, headID := uuid.NewString(), uuid.NewString()
	const insertInitial = `
		INSERT INTO content_change_proposals (id, content_item_id, base_revision_id, head_revision_id, author_id, initial_publication)
		VALUES ($1, $2, $3, $4, $5, TRUE)`
	if _, err = tx.Exec(ctx, insertInitial, proposalID, input.ContentItemID, item.revisionID, headID, input.AuthorID); err != nil {
		return domain.ChangeProposal{}, err
	}
	if err = s.insertProposalRevision(ctx, tx, proposalID, headID, "", nil, 1, source, ""); err != nil {
		return domain.ChangeProposal{}, err
	}
	target := attachmentRevision{revisionID: headID, proposalID: proposalID}
	if err = s.saveRevisionAttachments(ctx, tx, input.ContentItemID, input.AuthorID,
		target, attachmentRevision{revisionID: item.revisionID}, nil); err != nil {
		return domain.ChangeProposal{}, err
	}

	return finishProposalWrite(ctx, tx, proposalID, input.AuthorID)
}

func replayInitialPublication(ctx context.Context, tx pgx.Tx, input domain.SubmitInitialPublicationInput) (domain.ChangeProposal, error) {
	var id string
	err := tx.QueryRow(ctx, `
		SELECT id FROM content_change_proposals
		WHERE content_item_id = $1 AND base_revision_id = $2 AND author_id = $3 AND initial_publication AND status = 'merged'`,
		input.ContentItemID, input.ExpectedRevisionID, input.AuthorID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChangeProposal{}, service.ErrInvalidState
	}
	if err != nil {
		return domain.ChangeProposal{}, err
	}

	return finishProposalWrite(ctx, tx, id, input.AuthorID)
}

func (s *ContentItemStore) requireInitialDraftBindings(ctx context.Context, tx pgx.Tx, itemID, revisionID string) error {
	if s.manageAttachments {
		return s.requireAttachmentBindings(ctx, tx, itemID, revisionID)
	}
	var attached bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM content_assets WHERE content_item_id = $1 AND revision_id = $2)`,
		itemID, revisionID).Scan(&attached); err != nil {
		return err
	}
	if attached {
		return service.ErrMediaUnavailable
	}

	return nil
}

func (s *ContentItemStore) invalidateInitialPublication(ctx context.Context, tx pgx.Tx, itemID, actorID string) error {
	const closeOpen = `
		UPDATE content_change_proposals SET status = 'closed', closed_at = statement_timestamp()
		WHERE content_item_id = $1 AND author_id = $2 AND status = 'open' AND initial_publication
		RETURNING id, head_revision_id`

	return s.retireInvalidatedProposal(ctx, tx, tx.QueryRow(ctx, closeOpen, itemID, actorID))
}

func (s *ContentItemStore) retireInvalidatedProposal(ctx context.Context, tx pgx.Tx, row scanner) error {
	var id, headID string
	err := row.Scan(&id, &headID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	return s.retireClosedProposal(ctx, tx, id, headID)
}
