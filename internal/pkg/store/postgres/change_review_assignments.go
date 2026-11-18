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

const maxReviewCandidates = 64

// AssignReviewers принимает список подходящих кандидатов от доверенного источника
// в порядке рейтинга. Автор материала тоже должен входить в список, если он доступен.
func (s *ContentItemStore) AssignReviewers(ctx context.Context, input domain.ReviewAssignmentRequest) error {
	if len(input.RankedCandidateIDs) > maxReviewCandidates {
		return validator.Error{Message: "too many review candidates"}
	}
	topic, err := uuid.Parse(input.TopicID)
	if err != nil || topic == uuid.Nil {
		return validator.Error{Message: "invalid review topic"}
	}
	candidates := make([]string, 0, len(input.RankedCandidateIDs))
	for _, raw := range input.RankedCandidateIDs {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return validator.Error{Message: "invalid review candidate"}
		}
		candidates = append(candidates, id.String())
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		scope, err := lockReviewTarget(ctx, tx, input.ProposalID, input.RevisionID)
		if err != nil {
			return err
		}
		const setTopic = `
			WITH pinned AS (
			    UPDATE content_change_proposal_revisions SET review_topic_id = $3
			    WHERE proposal_id = $1 AND id = $2 AND review_topic_id IS NULL AND EXISTS (
			        SELECT 1 FROM content_item_topics WHERE content_item_id = $4 AND topic_id = $3
			    )
			    RETURNING 1
			)
			SELECT EXISTS (SELECT 1 FROM pinned) OR EXISTS (
			    SELECT 1 FROM content_change_proposal_revisions WHERE proposal_id = $1 AND id = $2 AND review_topic_id = $3
			)`
		var pinned bool
		err = tx.QueryRow(ctx, setTopic, input.ProposalID, scope.headID, topic, scope.itemID).Scan(&pinned)
		if err != nil {
			return err
		}
		if !pinned {
			return validator.Error{Message: "topic does not match the review revision"}
		}
		var status domain.ReviewRevisionStatus
		status.Assignments, err = loadActiveAssignments(ctx, tx, input.ProposalID, scope.headID)
		if err != nil {
			return err
		}
		state, wait := status.Outcome()
		if state != domain.ReviewWaitingCandidates || len(candidates) == 0 {
			return nil
		}
		used, err := loadUsedCandidates(ctx, tx, input.ProposalID, scope.headID, candidates)
		if err != nil {
			return err
		}
		const insert = `
			INSERT INTO content_change_review_assignments (proposal_id, revision_id, reviewer_id, slot)
			VALUES ($1, $2, $3, $4)
		`
		for _, assignment := range pickReviewAssignments(scope, wait, status.Assignments, candidates, used) {
			if _, err = tx.Exec(ctx, insert, input.ProposalID, scope.headID, assignment.ReviewerID, assignment.Slot); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *ContentItemStore) RevokeReviewAssignment(ctx context.Context, proposalID, revisionID, reviewerID string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		scope, err := lockReviewTarget(ctx, tx, proposalID, revisionID)
		if err != nil {
			return err
		}
		const revoke = `
			WITH revoked AS (
			    UPDATE content_change_review_assignments SET revoked_at = NOW()
			    WHERE proposal_id = $1 AND revision_id = $2 AND reviewer_id = $3 AND revoked_at IS NULL
			    RETURNING slot
			)
			UPDATE content_change_review_assignments SET revoked_at = NOW()
			WHERE proposal_id = $1 AND revision_id = $2 AND slot = 3 AND revoked_at IS NULL
			  AND EXISTS (SELECT 1 FROM revoked WHERE slot IN (1, 2))
		`
		_, err = tx.Exec(ctx, revoke, proposalID, scope.headID, reviewerID)
		return err
	})
}

func lockReviewTarget(ctx context.Context, q queryer, proposalID, revisionID string) (reviewScope, error) {
	scope, err := scanReviewScope(q.QueryRow(ctx, reviewScopeSQL+` FOR UPDATE OF p`, proposalID))
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
		for _, candidate := range candidates {
			_, seen := used[candidate]
			if seen || candidate == scope.author {
				continue
			}
			if scope.owner != "" && scope.owner != scope.author && (slot == 1) != (candidate == scope.owner) {
				continue
			}
			assignments = append(assignments, domain.ReviewAssignment{ReviewerID: candidate, Slot: slot})
			used[candidate] = struct{}{}
			break
		}
	}
	return assignments
}

func requireActiveReviewer(ctx context.Context, q queryer, actor domain.ReviewActor, headID string) error {
	const query = `
		SELECT 1 FROM content_change_review_assignments
		WHERE proposal_id = $1 AND revision_id = $2 AND reviewer_id = $3 AND revoked_at IS NULL
	`
	var found int
	err := q.QueryRow(ctx, query, actor.ProposalID, headID, actor.ActorID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrProposalNotFound
	}
	return err
}
