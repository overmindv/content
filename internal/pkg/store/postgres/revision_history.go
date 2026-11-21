package postgres

import (
	"context"
	"math"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

func (s *ContentItemStore) ListRevisionHistory(ctx context.Context, input domain.RevisionHistoryInput) ([]domain.RevisionSummary, error) {
	const query = `
		SELECT r.id, r.revision, r.format, r.message, r.created_by, r.created_at
		FROM content_items i
		LEFT JOIN LATERAL (
		    SELECT id, revision, format, left(message, 256) AS message, created_by, created_at
		    FROM content_revisions WHERE content_item_id = i.id AND revision < $2::bigint
		    ORDER BY revision DESC LIMIT $3
		) r ON TRUE
		WHERE i.id = $1 AND i.created_by = $4
		ORDER BY r.revision DESC`
	before := int64(input.BeforeRevision)
	if before == 0 {
		before = int64(math.MaxInt32) + 1
	}
	rows, err := s.db.Query(ctx, query, input.ContentItemID, before, input.PageSize+1, nullableString(input.ActorID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var revisions []domain.RevisionSummary
	found := false
	for rows.Next() {
		found = true
		var id, format, message, author *string
		var number *int32
		var createdAt *time.Time
		if scanErr := rows.Scan(&id, &number, &format, &message, &author, &createdAt); scanErr != nil {
			return nil, scanErr
		}
		if id != nil {
			revisions = append(revisions, domain.RevisionSummary{
				ID: *id, Revision: *number, Format: domain.ContentFormat(*format),
				Message: *message, CreatedBy: stringFromPtr(author), CreatedAt: *createdAt,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, service.ErrNotFound
	}

	return revisions, nil
}
