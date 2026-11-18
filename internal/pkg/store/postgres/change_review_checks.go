package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

const (
	reviewCheckIntervalSeconds = 10
	reviewVerificationSeconds  = 30
)

func (s *ContentItemStore) ClaimReviewCheck(ctx context.Context) (domain.ReviewCheck, bool, error) {
	const query = `
		WITH due AS (
		    SELECT r.proposal_id, r.id
		    FROM content_change_proposal_revisions AS r
		    WHERE r.review_due_at <= statement_timestamp()
		    ORDER BY r.review_due_at, r.proposal_id, r.id
		    LIMIT 1 FOR UPDATE OF r SKIP LOCKED
		), claimed AS (
		    UPDATE content_change_proposal_revisions AS r
		    SET review_due_at = CASE WHEN p.head_revision_id = r.id AND p.status = 'open'
		            THEN statement_timestamp() + ($2::integer * INTERVAL '1 second') END,
		        review_check_token = $1
		    FROM due JOIN content_change_proposals AS p ON p.id = due.proposal_id
		    WHERE r.proposal_id = due.proposal_id AND r.id = due.id
		    RETURNING r.proposal_id, r.id, r.review_check_token, r.review_topic_id, r.review_due_at, p.author_id, p.content_item_id
		)
		SELECT c.proposal_id, c.id, c.review_check_token,
		    COALESCE(c.review_topic_id, (
		        SELECT topic_id FROM content_item_topics WHERE content_item_id = i.id ORDER BY topic_id LIMIT 1
		    )), c.author_id, i.created_by,
		    ARRAY(SELECT a.reviewer_id FROM content_change_review_assignments AS a
		          WHERE a.proposal_id = c.proposal_id AND a.revision_id = c.id AND a.revoked_at IS NULL ORDER BY a.slot)
		FROM claimed AS c JOIN content_items AS i ON i.id = c.content_item_id WHERE c.review_due_at IS NOT NULL
	`
	var check domain.ReviewCheck
	var topic, owner *string
	var assignees []uuid.UUID
	err := s.db.QueryRow(ctx, query, uuid.NewString(), reviewCheckIntervalSeconds).Scan(
		&check.ProposalID, &check.RevisionID, &check.Token, &topic, &check.ProposalAuthorID, &owner, &assignees,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ReviewCheck{}, false, nil
	}
	if err != nil {
		return domain.ReviewCheck{}, false, err
	}
	check.TopicID, check.MaterialOwnerID = stringFromPtr(topic), stringFromPtr(owner)
	check.AssigneeIDs = make([]string, 0, len(assignees))
	for _, id := range assignees {
		check.AssigneeIDs = append(check.AssigneeIDs, id.String())
	}
	return check, true, nil
}

func (s *ContentItemStore) ApplyReviewCheck(ctx context.Context, check domain.ReviewCheck, result domain.ReviewCandidates) error {
	candidates, err := normalizeReviewCandidates(result.RankedIDs)
	if err != nil {
		return err
	}
	eligible := make(map[string]bool, len(result.EligibleIDs))
	for _, id := range result.EligibleIDs {
		eligible[id] = true
	}
	var revoked []string
	for _, id := range check.AssigneeIDs {
		if !eligible[id] {
			revoked = append(revoked, id)
		}
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		scope, err := lockReviewTarget(ctx, tx, check.ProposalID, check.RevisionID)
		if errors.Is(err, service.ErrInvalidState) || errors.Is(err, service.ErrProposalNotFound) || errors.Is(err, service.ErrRevisionConflict) {
			return nil
		}
		if err != nil {
			return err
		}

		// Запоздавший ответ не должен перезаписать более свежую проверку.
		const verify = `
			UPDATE content_change_proposal_revisions
			SET review_check_token = NULL,
			    review_verified_until = statement_timestamp() + ($4::integer * INTERVAL '1 second')
			WHERE proposal_id = $1 AND id = $2 AND review_check_token = $3
		`
		tag, err := tx.Exec(ctx, verify, check.ProposalID, scope.headID, check.Token, reviewVerificationSeconds)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		if err = pinReviewTopic(ctx, tx, check.ProposalID, scope, check.TopicID); err != nil {
			return err
		}
		if err = revokeAssignments(ctx, tx, check.ProposalID, scope.headID, revoked); err != nil {
			return err
		}
		return fillReviewSlots(ctx, tx, check.ProposalID, scope, candidates)
	})
}
