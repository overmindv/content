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
}

func loadReviewScope(ctx context.Context, q queryer, actor domain.ReviewActor, lock bool) (reviewScope, error) {
	query := `
		SELECT p.status, p.head_revision_id, p.author_id, i.created_by
		FROM content_change_proposals AS p
		JOIN content_items AS i ON i.id = p.content_item_id
		WHERE p.id = $1 AND ` + proposalReader
	if lock {
		query += ` FOR UPDATE OF p`
	}
	var scope reviewScope
	var owner *string
	err := q.QueryRow(ctx, query, actor.ProposalID, actor.ActorID).Scan(&scope.status, &scope.headID, &scope.author, &owner)
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
		if input.ActorID == scope.author || input.ActorID != scope.owner {
			return domain.ReviewDecision{}, service.ErrReviewNotAllowed
		}
		if input.RevisionID != scope.headID {
			return domain.ReviewDecision{}, service.ErrRevisionConflict
		}
		const insert = `
			INSERT INTO content_change_review_decisions (proposal_id, revision_id, reviewer_id, decision, reason)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (proposal_id, revision_id, reviewer_id) DO NOTHING
		`
		if _, err := tx.Exec(ctx, insert, input.ProposalID, input.RevisionID, input.ActorID, input.Decision, input.Reason); err != nil {
			return domain.ReviewDecision{}, err
		}
		const read = `
			SELECT reviewer_id, decision, reason, created_at FROM content_change_review_decisions
			WHERE proposal_id = $1 AND revision_id = $2 AND reviewer_id = $3
		`
		decision, err := scanReviewDecision(tx.QueryRow(ctx, read, input.ProposalID, input.RevisionID, input.ActorID))
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
		SELECT r.id, (p.status = 'open' AND r.id = p.head_revision_id),
		       d.reviewer_id, d.decision, d.reason, d.created_at
		FROM content_change_proposals AS p
		JOIN content_items AS i ON i.id = p.content_item_id
		LEFT JOIN content_change_proposal_revisions AS r ON r.proposal_id = p.id AND r.id = COALESCE($3, p.head_revision_id)
		LEFT JOIN content_change_review_decisions AS d ON d.proposal_id = p.id AND d.revision_id = r.id
		WHERE p.id = $1 AND ` + proposalReader + ` ORDER BY d.created_at, d.reviewer_id`
	rows, err := s.db.Query(ctx, query, ref.ProposalID, ref.ActorID, nullableString(ref.RevisionID))
	if err != nil {
		return domain.ReviewRevisionStatus{}, err
	}
	defer rows.Close()
	var status domain.ReviewRevisionStatus
	for rows.Next() {
		var decision domain.ReviewDecision
		var revisionID, reviewer, kind, reason *string
		var current *bool
		var createdAt *time.Time
		if err = rows.Scan(&revisionID, &current, &reviewer, &kind, &reason, &createdAt); err != nil {
			return domain.ReviewRevisionStatus{}, err
		}
		if revisionID == nil {
			return domain.ReviewRevisionStatus{}, service.ErrProposalRevisionNotFound
		}
		status.RevisionID, status.Current = *revisionID, *current
		if reviewer != nil {
			decision.ReviewerID, decision.Decision, decision.Reason, decision.CreatedAt = *reviewer, domain.ReviewDecisionKind(*kind), *reason, *createdAt
			status.Decisions = append(status.Decisions, decision)
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

func scanReviewDecision(row scanner) (domain.ReviewDecision, error) {
	var d domain.ReviewDecision
	err := row.Scan(&d.ReviewerID, &d.Decision, &d.Reason, &d.CreatedAt)

	return d, err
}
