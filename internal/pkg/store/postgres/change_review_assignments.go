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

func pinReviewTopic(ctx context.Context, tx pgx.Tx, proposalID string, scope reviewScope, topicID string) error {
	const setTopic = `
			WITH pinned AS (
			    UPDATE content_change_proposal_revisions SET review_topic_id = $3
			    WHERE proposal_id = $1 AND id = $2 AND review_topic_id IS NULL AND EXISTS (
			        SELECT 1 FROM content_item_topics WHERE content_item_id = $4 AND topic_id = $3
			    )
			    RETURNING 1
			)
			SELECT EXISTS (SELECT 1 FROM content_item_topics WHERE content_item_id = $4 AND topic_id = $3)
			   AND (EXISTS (SELECT 1 FROM pinned) OR EXISTS (
			       SELECT 1 FROM content_change_proposal_revisions WHERE proposal_id = $1 AND id = $2 AND review_topic_id = $3
			   ))`
	var pinned bool
	if err := tx.QueryRow(ctx, setTopic, proposalID, scope.headID, topicID, scope.itemID).Scan(&pinned); err != nil {
		return err
	}
	if !pinned {
		return validator.Error{Message: "topic does not match the review revision"}
	}

	return nil
}

func fillReviewSlots(ctx context.Context, tx pgx.Tx, proposalID string, scope reviewScope, candidates []string) error {
	active, err := loadActiveAssignments(ctx, tx, proposalID, scope.headID)
	if err != nil {
		return err
	}
	status := domain.ReviewRevisionStatus{Assignments: active}
	state, wait := status.Outcome()
	if state != domain.ReviewWaitingCandidates || len(candidates) == 0 {
		return nil
	}
	used, err := loadUsedCandidates(ctx, tx, proposalID, scope.headID, candidates)
	if err != nil {
		return err
	}
	const insert = `
			INSERT INTO content_change_review_assignments (proposal_id, revision_id, reviewer_id, slot)
			VALUES ($1, $2, $3, $4)
		`
	for _, assignment := range pickReviewAssignments(scope, wait, active, candidates, used) {
		if _, err = tx.Exec(ctx, insert, proposalID, scope.headID, assignment.ReviewerID, assignment.Slot); err != nil {
			return err
		}
	}

	return nil
}

// AssignReviewers принимает кандидатов в порядке рейтинга из доверенного источника.
func (s *ContentItemStore) AssignReviewers(ctx context.Context, input domain.ReviewAssignmentRequest) error {
	candidates, err := normalizeReviewCandidates(input.RankedCandidateIDs)
	if err != nil {
		return err
	}
	topic, err := uuid.Parse(input.TopicID)
	if err != nil || topic == uuid.Nil {
		return validator.Error{Message: "invalid review topic"}
	}

	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		scope, err := lockReviewTarget(ctx, tx, input.ProposalID, input.RevisionID)
		if err != nil {
			return err
		}
		if err = pinReviewTopic(ctx, tx, input.ProposalID, scope, topic.String()); err != nil {
			return err
		}
		if err = fillReviewSlots(ctx, tx, input.ProposalID, scope, candidates); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE content_change_proposal_revisions
			SET review_check_token = NULL,
			    review_verified_until = statement_timestamp() + ($3::integer * INTERVAL '1 second')
			WHERE proposal_id = $1 AND id = $2
		`, input.ProposalID, scope.headID, reviewVerificationSeconds)

		return err
	})
}

func normalizeReviewCandidates(raw []string) ([]string, error) {
	if len(raw) > domain.MaxReviewCandidates {
		return nil, validator.Error{Message: "too many review candidates"}
	}
	result := make([]string, 0, len(raw))
	for _, value := range raw {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil {
			return nil, validator.Error{Message: "invalid review candidate"}
		}
		result = append(result, id.String())
	}

	return result, nil
}

func (s *ContentItemStore) RevokeReviewAssignment(ctx context.Context, proposalID, revisionID, reviewerID string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := lockReviewTarget(ctx, tx, proposalID, revisionID); err != nil {
			return err
		}
		if err := revokeAssignments(ctx, tx, proposalID, revisionID, []string{reviewerID}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE content_change_proposal_revisions SET review_check_token = NULL, review_due_at = statement_timestamp()
			WHERE proposal_id = $1 AND id = $2 AND review_due_at IS NOT NULL
		`, proposalID, revisionID)

		return err
	})
}

func revokeAssignments(ctx context.Context, tx pgx.Tx, proposalID, revisionID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	const revoke = `
			WITH revoked AS (
			    UPDATE content_change_review_assignments SET revoked_at = NOW()
			    WHERE proposal_id = $1 AND revision_id = $2 AND reviewer_id = ANY($3::uuid[]) AND revoked_at IS NULL
			    RETURNING slot
			)
			UPDATE content_change_review_assignments SET revoked_at = NOW()
			WHERE proposal_id = $1 AND revision_id = $2 AND slot = 3 AND revoked_at IS NULL
			  AND reviewer_id <> ALL($3::uuid[])
			  AND EXISTS (SELECT 1 FROM revoked WHERE slot IN (1, 2))
		`
	_, err := tx.Exec(ctx, revoke, proposalID, revisionID, ids)

	return err
}

func lockReviewTarget(ctx context.Context, q queryer, proposalID, revisionID string) (reviewScope, error) {
	scope, err := scanReviewScope(q.QueryRow(ctx, reviewScopeSQL+` AND (`+activeReviewTarget+`) FOR UPDATE OF p`, proposalID))
	if err != nil {
		return reviewScope{}, err
	}
	if scope.status != domain.ChangeProposalOpen {
		return reviewScope{}, service.ErrInvalidState
	}
	if scope.headID != revisionID {
		return reviewScope{}, service.ErrRevisionConflict
	}

	return scope, nil
}

func loadActiveAssignments(ctx context.Context, q queryer, proposalID, revisionID string) ([]domain.ReviewAssignment, error) {
	const query = `
		SELECT reviewer_id, slot, COALESCE(decision, '')
		FROM content_change_review_assignments WHERE proposal_id = $1 AND revision_id = $2 AND revoked_at IS NULL
	`
	rows, err := q.Query(ctx, query, proposalID, revisionID)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ReviewAssignment, error) {
		var assignment domain.ReviewAssignment
		err := row.Scan(&assignment.ReviewerID, &assignment.Slot, &assignment.Decision)

		return assignment, err
	})
}

func loadUsedCandidates(ctx context.Context, q queryer, proposalID, revisionID string, candidates []string) (map[string]struct{}, error) {
	const query = `
		SELECT reviewer_id FROM content_change_review_assignments
		WHERE proposal_id = $1 AND revision_id = $2 AND reviewer_id = ANY($3::uuid[])
	`
	rows, err := q.Query(ctx, query, proposalID, revisionID, candidates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := make(map[string]struct{}, len(candidates))
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		used[id] = struct{}{}
	}

	return used, rows.Err()
}

func pickReviewAssignments(scope reviewScope, wait domain.ReviewWaitReason, active []domain.ReviewAssignment, candidates []string, used map[string]struct{}) []domain.ReviewAssignment {
	var occupied [4]bool
	for _, a := range active {
		occupied[a.Slot] = true
	}
	first, last := int16(1), int16(2)
	if wait == domain.ReviewNeedsTiebreaker {
		first, last = 3, 3
	}
	assignments := make([]domain.ReviewAssignment, 0, 2)
	for slot := first; slot <= last; slot++ {
		if occupied[slot] {
			continue
		}
		var selected string
		for _, candidate := range candidates {
			_, seen := used[candidate]
			if seen || candidate == scope.author {
				continue
			}
			if selected == "" {
				selected = candidate
			}
			if slot < 3 && candidate == scope.owner {
				selected = candidate

				break
			}
			if slot == 3 {
				break
			}
		}
		if selected != "" {
			assignments = append(assignments, domain.ReviewAssignment{ReviewerID: selected, Slot: slot})
			used[selected] = struct{}{}
		}
	}

	return assignments
}

func requireActiveReviewer(ctx context.Context, q queryer, actor domain.ReviewActor, headID string) error {
	const query = `
		SELECT 1 FROM content_change_review_assignments AS a
		JOIN content_change_proposal_revisions AS r ON r.proposal_id = a.proposal_id AND r.id = a.revision_id
		WHERE a.proposal_id = $1 AND a.revision_id = $2 AND a.reviewer_id = $3 AND a.revoked_at IS NULL
		  AND r.review_verified_until > statement_timestamp()
	`
	var found int
	err := q.QueryRow(ctx, query, actor.ProposalID, headID, actor.ActorID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrProposalNotFound
	}

	return err
}
