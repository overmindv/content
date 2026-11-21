package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/validator"
)

type proposalMerge struct {
	itemID   string
	status   domain.ChangeProposalStatus
	headID   string
	baseID   string
	authorID string
	mergedID *string
}

type mergeSource struct {
	text, message string
	attachments   attachmentRevision
	review        domain.ReviewRevisionStatus
}

func (s *ContentItemStore) MergeChangeProposal(ctx context.Context, input domain.MergeChangeProposalInput) (domain.MergeChangeProposalResult, error) {
	var result domain.MergeChangeProposalResult
	var pending bool
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		proposal, material, err := lockProposalMerge(ctx, tx, input)
		if err != nil {
			return err
		}
		if proposal.headID != input.ExpectedHeadID {
			return service.ErrRevisionConflict
		}
		result.ContentItemID = proposal.itemID
		if proposal.status == domain.ChangeProposalMerged && proposal.mergedID != nil {
			result.PublishedRevisionID = *proposal.mergedID

			return nil
		}
		if proposal.status != domain.ChangeProposalOpen || material.status != domain.ContentStatusPublished || material.hidden {
			return service.ErrInvalidState
		}
		if proposal.baseID != material.publishedRevisionID {
			return service.ErrMergeConflict
		}
		source, err := loadMergeSource(ctx, tx, input.ProposalID, proposal)
		if err != nil {
			return err
		}
		revisionID, ready, err := s.prepareMergeRevision(ctx, tx, input, proposal, source.attachments)
		if err != nil {
			return err
		}
		if !ready {
			pending = true

			return nil
		}
		if err = s.publishMergedRevision(ctx, tx, input, proposal, source, revisionID); err != nil {
			return err
		}
		result.PublishedRevisionID = revisionID

		return nil
	})
	if err != nil {
		return domain.MergeChangeProposalResult{}, err
	}
	if pending {
		return domain.MergeChangeProposalResult{}, service.ErrAttachmentsPending
	}

	return result, nil
}

func lockProposalMerge(ctx context.Context, tx pgx.Tx, input domain.MergeChangeProposalInput) (proposalMerge, lockedContentItem, error) {
	var p proposalMerge
	err := tx.QueryRow(ctx, `SELECT content_item_id FROM content_change_proposals WHERE id = $1`,
		input.ProposalID).Scan(&p.itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, lockedContentItem{}, service.ErrProposalNotFound
	}
	if err != nil {
		return p, lockedContentItem{}, err
	}
	// Порядок блокировок совпадает с редактированием и удалением материала.
	material, err := lockOwnedContentItem(ctx, tx, p.itemID, input.ActorID)
	if errors.Is(err, service.ErrNotFound) {
		return p, lockedContentItem{}, service.ErrProposalNotFound
	}
	if err != nil {
		return p, lockedContentItem{}, err
	}
	const query = `
		SELECT p.status, p.head_revision_id, COALESCE(r.base_revision_id, p.base_revision_id), p.author_id, p.merged_revision_id
		FROM content_change_proposals p
		JOIN content_change_proposal_revisions r ON r.proposal_id = p.id AND r.id = p.head_revision_id
		WHERE p.id = $1 AND p.content_item_id = $2 FOR UPDATE OF p`
	err = tx.QueryRow(ctx, query, input.ProposalID, p.itemID).Scan(&p.status, &p.headID, &p.baseID, &p.authorID, &p.mergedID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = service.ErrProposalNotFound
	}

	return p, material, err
}

func loadMergeSource(ctx context.Context, tx pgx.Tx, proposalID string, proposal proposalMerge) (mergeSource, error) {
	const query = `
		SELECT CASE WHEN octet_length(r.source) <= $4 THEN r.source END, r.message, r.attachments_snapshot,
		       (SELECT t.topic_id FROM content_item_topics t WHERE t.content_item_id = $3 AND t.topic_id = r.review_topic_id),
		       EXISTS (SELECT 1 FROM content_change_review_threads t
		               WHERE t.proposal_id = $1 AND t.revision_id = $2 AND t.resolved_at IS NULL)
		FROM content_change_proposal_revisions r WHERE r.proposal_id = $1 AND r.id = $2`
	var result mergeSource
	var text, topicID *string
	var snapshot, unresolved bool
	if err := tx.QueryRow(ctx, query, proposalID, proposal.headID, proposal.itemID, validator.MaxSourceBytes).
		Scan(&text, &result.message, &snapshot, &topicID, &unresolved); err != nil {
		return result, err
	}
	if topicID == nil {
		return result, service.ErrReviewRequired
	}
	assignments, err := loadActiveAssignments(ctx, tx, proposalID, proposal.headID)
	if err != nil {
		return result, err
	}
	result.review = domain.ReviewRevisionStatus{TopicID: *topicID, Assignments: assignments}
	if outcome, _ := result.review.Outcome(); outcome != domain.ReviewApproved {
		return result, service.ErrReviewRequired
	}
	if unresolved {
		return result, service.ErrUnresolvedReviewThreads
	}
	if text == nil {
		return result, validator.ValidateSourceSize(validator.MaxSourceBytes + 1)
	}
	photos, err := validator.PublishablePhotoIDs(domain.ContentFormatMarkdown, *text)
	if err != nil {
		return result, err
	}
	result.text = *text
	result.attachments = attachmentRevision{revisionID: proposal.baseID}
	if snapshot {
		result.attachments = attachmentRevision{proposalID: proposalID, revisionID: proposal.headID}
	}
	if err = requireRevisionPhotos(ctx, tx, proposal.itemID, result.attachments, photos); err != nil {
		return result, err
	}

	return result, nil
}

const mergeAttachmentSource = ` FROM content_assets WHERE content_item_id = $1
	AND (($3::uuid IS NULL AND revision_id = $2) OR (proposal_id = $3 AND proposal_revision_id = $2))`

func (s *ContentItemStore) publishMergedRevision(ctx context.Context, tx pgx.Tx, input domain.MergeChangeProposalInput,
	p proposalMerge, source mergeSource, revisionID string) error {
	const insertRevision = `
		INSERT INTO content_revisions (id, content_item_id, revision, format, source, source_hash, message, created_by)
		VALUES ($1, $2, (SELECT COALESCE(MAX(revision), 0) + 1 FROM content_revisions WHERE content_item_id = $2),
		        'markdown', $3, $4, $5, $6)`
	if _, err := tx.Exec(ctx, insertRevision, revisionID, p.itemID, source.text, hashSource(source.text), source.message, p.authorID); err != nil {
		return err
	}
	const copyAssets = `
		INSERT INTO content_assets (id, content_item_id, revision_id, asset_id, kind, title, position)
		SELECT gen_random_uuid(), $1, $4, asset_id, kind, title, position` + mergeAttachmentSource + `
		ORDER BY position, id LIMIT $5`
	if _, err := tx.Exec(ctx, copyAssets, p.itemID, source.attachments.revisionID, nullableString(source.attachments.proposalID),
		revisionID, validator.MaxAssets); err != nil {
		return err
	}
	if err := promoteMergeBinding(ctx, tx, input.ProposalID, revisionID); err != nil {
		return err
	}
	if err := s.retireAttachmentBindings(ctx, tx, input.ProposalID, true); err != nil {
		return err
	}
	const markMerged = `
		UPDATE content_change_proposals SET status = 'merged', merged_revision_id = $2, closed_at = statement_timestamp()
		WHERE id = $1`
	if _, err := tx.Exec(ctx, markMerged, input.ProposalID, revisionID); err != nil {
		return err
	}
	const publish = `
		UPDATE content_items SET published_revision_id = $2,
		    current_revision_id = CASE WHEN current_revision_id = published_revision_id THEN $2 ELSE current_revision_id END,
		    published_at = statement_timestamp(), updated_at = statement_timestamp(), updated_by = $3
		WHERE id = $1`
	if _, err := tx.Exec(ctx, publish, p.itemID, revisionID, input.ActorID); err != nil {
		return err
	}

	return s.recordMergePublication(ctx, tx, input.ProposalID, p, source.review)
}

func (s *ContentItemStore) recordMergePublication(ctx context.Context, tx pgx.Tx, proposalID string, p proposalMerge, review domain.ReviewRevisionStatus) error {
	item, err := s.get(ctx, tx, p.itemID, "")
	if err != nil {
		return err
	}
	if err = s.enqueueContentItem(ctx, tx, eventContentItemUpdated, item); err != nil {
		return err
	}
	version, err := s.recordMaterialState(ctx, tx, item, false)
	if err != nil {
		return err
	}

	return s.enqueueEditorialActivity(ctx, tx, item, version, proposalID, p, review)
}
