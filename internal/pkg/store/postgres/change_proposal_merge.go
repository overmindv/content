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
	"github.com/overmindv/content/pkg/api/activity"
)

type proposalMaterial struct {
	id string
	lockedContentItem
}

func lockProposalMaterial(ctx context.Context, tx pgx.Tx, proposalID, actorID string, rebase bool) (proposalMaterial, error) {
	// Блокируем материал раньше предложения, как при удалении материала.
	query := `
		SELECT i.id, i.status, i.current_revision_id, i.published_revision_id, i.moderation_hidden
		FROM content_items AS i
		WHERE i.id = (SELECT p.content_item_id FROM content_change_proposals AS p
		              WHERE p.id = $1 AND (NOT $3::boolean OR p.author_id = $2))
		  AND ($3 OR i.created_by = $2)`
	if rebase {
		query += ` FOR SHARE OF i`
	} else {
		query += ` FOR UPDATE OF i`
	}
	var material proposalMaterial
	var current, published *string
	err := tx.QueryRow(ctx, query, proposalID, actorID, rebase).Scan(&material.id, &material.status, &current, &published, &material.hidden)
	if errors.Is(err, pgx.ErrNoRows) {
		return proposalMaterial{}, service.ErrProposalNotFound
	}
	material.revisionID, material.publishedRevisionID = stringFromPtr(current), stringFromPtr(published)
	return material, err
}

func (s *ContentItemStore) MergeChangeProposal(ctx context.Context, input domain.MergeChangeProposalInput) (domain.MergeChangeProposalResult, error) {
	var result domain.MergeChangeProposalResult
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		material, err := lockProposalMaterial(ctx, tx, input.ProposalID, input.ActorID, false)
		if err != nil {
			return err
		}
		const lockProposal = `
			SELECT status, head_revision_id, base_revision_id, author_id, merged_revision_id
			FROM content_change_proposals WHERE id = $1 AND content_item_id = $2 FOR UPDATE`
		var state domain.ChangeProposalStatus
		var head, originalBase, author string
		var merged *string
		err = tx.QueryRow(ctx, lockProposal, input.ProposalID, material.id).Scan(&state, &head, &originalBase, &author, &merged)
		if errors.Is(err, pgx.ErrNoRows) {
			return service.ErrProposalNotFound
		}
		if err != nil {
			return err
		}
		if head != input.ExpectedHeadID {
			return service.ErrRevisionConflict
		}
		if state == domain.ChangeProposalMerged && merged != nil {
			result = domain.MergeChangeProposalResult{ContentItemID: material.id, PublishedRevisionID: *merged}
			return nil
		}
		if state != domain.ChangeProposalOpen || material.status != domain.ContentStatusPublished || material.hidden {
			return service.ErrInvalidState
		}
		review, err := validateMergeReview(ctx, tx, input, material, originalBase)
		if err != nil {
			return err
		}
		source := attachmentRevision{revisionID: material.publishedRevisionID}
		if review.attachmentsSnapshot {
			source = attachmentRevision{revisionID: input.ExpectedHeadID, proposalID: input.ProposalID}
		}
		if err = s.requireAttachmentBindings(ctx, tx, material.id, source); err != nil {
			return err
		}
		revisionID, err := insertMergedRevision(ctx, tx, input, material, author)
		if err != nil {
			return err
		}
		if err = s.saveRevisionAttachments(ctx, tx, material.id, author, attachmentRevision{revisionID: revisionID}, source, nil); err != nil {
			return err
		}
		if err = publishMergedRevision(ctx, tx, input, material.id, revisionID); err != nil {
			return err
		}
		item, err := s.get(ctx, tx, material.id, "")
		if err != nil {
			return err
		}
		if err = s.enqueueContentItem(ctx, tx, eventContentItemUpdated, item); err != nil {
			return err
		}
		version, err := s.recordMaterialState(ctx, tx, item, activity.MaterialPublished, "")
		if err != nil {
			return err
		}
		if err = s.enqueueEditorialActivity(ctx, tx, item, version, input, author, review); err != nil {
			return err
		}
		result = domain.MergeChangeProposalResult{ContentItemID: material.id, PublishedRevisionID: revisionID}
		return nil
	})
	if err != nil {
		return domain.MergeChangeProposalResult{}, err
	}
	return result, nil
}

type mergeReview struct {
	topicID             string
	assignments         []domain.ReviewAssignment
	attachmentsSnapshot bool
}

func validateMergeReview(ctx context.Context, tx pgx.Tx, input domain.MergeChangeProposalInput, material proposalMaterial, originalBase string) (mergeReview, error) {
	const query = `
		SELECT COALESCE(base_revision_id, $3), COALESCE(review_verified_until > statement_timestamp(), false),
		       review_topic_id, attachments_snapshot, ` + unresolvedDiscussions + `
		FROM content_change_proposal_revisions WHERE proposal_id = $1 AND id = $2`
	var base string
	var verified, unresolved bool
	var topic *string
	var attachmentsSnapshot bool
	if err := tx.QueryRow(ctx, query, input.ProposalID, input.ExpectedHeadID, originalBase).
		Scan(&base, &verified, &topic, &attachmentsSnapshot, &unresolved); err != nil {
		return mergeReview{}, err
	}
	if base != material.publishedRevisionID {
		return mergeReview{}, service.ErrMergeConflict
	}
	if unresolved {
		return mergeReview{}, service.ErrUnresolvedDiscussions
	}
	if !verified || topic == nil {
		return mergeReview{}, service.ErrReviewRequired
	}
	assignments, err := loadActiveAssignments(ctx, tx, input.ProposalID, input.ExpectedHeadID)
	if err != nil {
		return mergeReview{}, err
	}
	if outcome, _ := (domain.ReviewRevisionStatus{Assignments: assignments}).Outcome(); outcome != domain.ReviewApproved {
		return mergeReview{}, service.ErrReviewRequired
	}
	return mergeReview{topicID: *topic, assignments: assignments, attachmentsSnapshot: attachmentsSnapshot}, nil
}

func insertMergedRevision(ctx context.Context, tx pgx.Tx, input domain.MergeChangeProposalInput, material proposalMaterial, author string) (string, error) {
	const load = `
		SELECT b.format, CASE WHEN octet_length(r.source) <= $5 THEN r.source END, r.message
		FROM content_change_proposal_revisions AS r
		JOIN content_revisions AS b ON b.content_item_id = $3 AND b.id = $4
		WHERE r.proposal_id = $1 AND r.id = $2`
	var format domain.ContentFormat
	var source *string
	var message string
	if err := tx.QueryRow(ctx, load, input.ProposalID, input.ExpectedHeadID, material.id, material.publishedRevisionID, validator.MaxSourceBytes).
		Scan(&format, &source, &message); err != nil {
		return "", err
	}
	if source == nil {
		return "", validator.ValidateSourceSize(validator.MaxSourceBytes + 1)
	}
	if err := validator.ValidatePublishableSource(format, *source); err != nil {
		return "", err
	}
	revisionID := uuid.NewString()
	const insert = `
		INSERT INTO content_revisions (id, content_item_id, revision, format, source, source_hash, message, created_by)
		VALUES ($1, $2, (SELECT COALESCE(MAX(revision), 0) + 1 FROM content_revisions WHERE content_item_id = $2),
		        $3, $4, $5, $6, $7)`
	if _, err := tx.Exec(ctx, insert, revisionID, material.id, format, *source, hashSource(*source), message, author); err != nil {
		return "", err
	}
	return revisionID, nil
}

func publishMergedRevision(ctx context.Context, tx pgx.Tx, input domain.MergeChangeProposalInput, itemID, revisionID string) error {
	const publish = `
		UPDATE content_items SET published_revision_id = $2,
		    current_revision_id = CASE WHEN current_revision_id = published_revision_id THEN $2 ELSE current_revision_id END,
		    published_at = statement_timestamp(), updated_at = statement_timestamp(), updated_by = $3
		WHERE id = $1 AND EXISTS (
		    SELECT 1 FROM content_change_proposal_revisions
		    WHERE proposal_id = $4 AND id = $5 AND review_verified_until > statement_timestamp()
		)
		RETURNING published_at`
	var publishedAt time.Time
	err := tx.QueryRow(ctx, publish, itemID, revisionID, input.ActorID, input.ProposalID, input.ExpectedHeadID).Scan(&publishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrReviewRequired
	}
	if err != nil {
		return err
	}
	const markMerged = `
		UPDATE content_change_proposals SET status = 'merged', closed_at = $2, merged_revision_id = $3 WHERE id = $1`
	if _, err = tx.Exec(ctx, markMerged, input.ProposalID, publishedAt, revisionID); err != nil {
		return err
	}
	return cancelReviewCheck(ctx, tx, input.ProposalID, input.ExpectedHeadID)
}
