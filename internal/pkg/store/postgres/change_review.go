package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

type reviewScope struct {
	status domain.ChangeProposalStatus
	headID string
	author string
	owner  string
	itemID string
}

const reviewScopeSQL = `
	SELECT p.status, p.head_revision_id, p.author_id, i.created_by, p.content_item_id
	FROM content_change_proposals AS p
	JOIN content_items AS i ON i.id = p.content_item_id
	WHERE p.id = $1`

func loadReviewScope(ctx context.Context, q queryer, actor domain.ReviewActor, lock bool) (reviewScope, error) {
	query := reviewScopeSQL + ` AND ` + proposalReader
	if lock {
		query += ` FOR UPDATE OF p`
	}

	return scanReviewScope(q.QueryRow(ctx, query, actor.ProposalID, actor.ActorID))
}

func scanReviewScope(row scanner) (reviewScope, error) {
	var scope reviewScope
	var owner *string
	err := row.Scan(&scope.status, &scope.headID, &scope.author, &owner, &scope.itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewScope{}, service.ErrProposalNotFound
	}
	if err != nil {
		return reviewScope{}, err
	}
	scope.owner = stringFromPtr(owner)

	return scope, nil
}

func writeReview[T any](ctx context.Context, store *ContentItemStore, actor domain.ReviewActor, write func(pgx.Tx, reviewScope) (T, error)) (T, error) {
	var value T
	err := pgx.BeginFunc(ctx, store.db, func(tx pgx.Tx) error {
		scope, err := loadReviewScope(ctx, tx, actor, true)
		if err != nil {
			return err
		}
		if scope.status != domain.ChangeProposalOpen {
			return service.ErrInvalidState
		}
		if actor.ActorID != scope.author && actor.ActorID != scope.owner {
			// Отзыв назначения мог завершиться, пока запрос ждал блокировку предложения.
			if accessErr := requireActiveReviewer(ctx, tx, actor, scope.headID); accessErr != nil {
				return accessErr
			}
		}
		value, err = write(tx, scope)

		return err
	})
	if err != nil {
		var zero T

		return zero, err
	}

	return value, nil
}

func (s *ContentItemStore) SubmitReviewDecision(ctx context.Context, input domain.SubmitReviewDecisionInput) (domain.ReviewDecision, error) {
	return writeReview(ctx, s, input.ReviewActor, func(tx pgx.Tx, scope reviewScope) (domain.ReviewDecision, error) {
		if input.ActorID == scope.author {
			return domain.ReviewDecision{}, service.ErrReviewNotAllowed
		}
		if input.RevisionID != scope.headID {
			return domain.ReviewDecision{}, service.ErrRevisionConflict
		}
		const save = `
			WITH voted AS (
			    UPDATE content_change_review_assignments SET decision = $4, reason = $5, decided_at = NOW()
			    WHERE proposal_id = $1 AND revision_id = $2 AND reviewer_id = $3 AND revoked_at IS NULL AND decision IS NULL
			      AND EXISTS (
			          SELECT 1 FROM content_change_proposal_revisions
			          WHERE proposal_id = $1 AND id = $2 AND review_verified_until > statement_timestamp()
			      )
			    RETURNING reviewer_id, slot, decision, reason, decided_at
			), scheduled AS (
			    UPDATE content_change_proposal_revisions
			    SET review_due_at = LEAST(review_due_at, statement_timestamp()), review_check_token = NULL
			    WHERE proposal_id = $1 AND id = $2 AND review_due_at IS NOT NULL AND EXISTS (
			        SELECT 1 FROM voted
			        JOIN content_change_review_assignments AS other ON other.proposal_id = $1 AND other.revision_id = $2
			        WHERE voted.slot IN (1, 2) AND other.slot IN (1, 2) AND other.reviewer_id <> voted.reviewer_id
			          AND other.revoked_at IS NULL AND other.decision <> voted.decision
			    )
			)
			SELECT reviewer_id, decision, reason, decided_at FROM voted
			UNION ALL
			SELECT reviewer_id, decision, reason, decided_at FROM content_change_review_assignments
			WHERE proposal_id = $1 AND revision_id = $2 AND reviewer_id = $3 AND revoked_at IS NULL AND decision IS NOT NULL
			  AND EXISTS (
			      SELECT 1 FROM content_change_proposal_revisions
			      WHERE proposal_id = $1 AND id = $2 AND review_verified_until > statement_timestamp()
			  )
		`
		var decision domain.ReviewDecision
		err := tx.QueryRow(ctx, save, input.ProposalID, input.RevisionID, input.ActorID, input.Decision, input.Reason).
			Scan(&decision.ReviewerID, &decision.Decision, &decision.Reason, &decision.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReviewDecision{}, service.ErrReviewNotAllowed
		}
		if err != nil {
			return domain.ReviewDecision{}, err
		}
		if decision.Decision != input.Decision || decision.Reason != input.Reason {
			return domain.ReviewDecision{}, service.ErrReviewDecisionExists
		}

		return decision, nil
	})
}

func (s *ContentItemStore) GetReviewRevisionStatus(ctx context.Context, ref domain.ReviewRevisionRef) (domain.ReviewRevisionStatus, error) {
	query := `
		SELECT r.id, (p.status = 'open' AND r.id = p.head_revision_id), r.review_topic_id,
		       COALESCE(r.review_verified_until > statement_timestamp(), false),
		       vote.reviewer_id, vote.slot, vote.decision, vote.reason, vote.decided_at
		FROM content_change_proposals AS p
		JOIN content_items AS i ON i.id = p.content_item_id
		LEFT JOIN content_change_proposal_revisions AS r ON r.proposal_id = p.id AND r.id = COALESCE($3, p.head_revision_id)
		LEFT JOIN LATERAL (
			SELECT a.reviewer_id, a.slot, a.decision, a.reason, a.decided_at
			FROM content_change_review_assignments AS a
			WHERE a.proposal_id = p.id AND a.revision_id = r.id AND a.revoked_at IS NULL
			UNION ALL
			SELECT d.reviewer_id, 0::smallint, d.decision, d.reason, d.created_at
			FROM content_change_review_decisions AS d WHERE d.proposal_id = p.id AND d.revision_id = r.id
		) AS vote ON true
		WHERE p.id = $1 AND ` + proposalReader + ` ORDER BY vote.slot`
	rows, err := s.db.Query(ctx, query, ref.ProposalID, ref.ActorID, nullableString(ref.RevisionID))
	if err != nil {
		return domain.ReviewRevisionStatus{}, err
	}
	defer rows.Close()
	var status domain.ReviewRevisionStatus
	for rows.Next() {
		var revisionID, topicID, reviewer, kind, reason *string
		var current *bool
		var verified bool
		var slot *int16
		var createdAt *time.Time
		if err = rows.Scan(&revisionID, &current, &topicID, &verified, &reviewer, &slot, &kind, &reason, &createdAt); err != nil {
			return domain.ReviewRevisionStatus{}, err
		}
		if revisionID == nil {
			return domain.ReviewRevisionStatus{}, service.ErrProposalRevisionNotFound
		}
		status.RevisionID, status.Current, status.TopicID = *revisionID, *current, stringFromPtr(topicID)
		status.EligibilityStale = status.Current && !verified
		if slot != nil && *slot != 0 {
			status.Assignments = append(status.Assignments, domain.ReviewAssignment{
				ReviewerID: *reviewer, Slot: *slot, Decision: domain.ReviewDecisionKind(stringFromPtr(kind)),
			})
		}
		if kind != nil {
			status.Decisions = append(status.Decisions, domain.ReviewDecision{
				ReviewerID: *reviewer, Decision: domain.ReviewDecisionKind(*kind), Reason: *reason, CreatedAt: *createdAt, Legacy: *slot == 0,
			})
		}
	}
	if err = rows.Err(); err != nil {
		return domain.ReviewRevisionStatus{}, err
	}
	if status.RevisionID == "" {
		return domain.ReviewRevisionStatus{}, service.ErrProposalNotFound
	}

	return status, nil
}
