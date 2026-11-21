package postgres

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/validator"
)

const proposalReader = `(p.author_id = $2 OR i.created_by = $2 OR (
	p.status = 'open' AND EXISTS (
		SELECT 1 FROM content_change_review_assignments AS a
		WHERE a.proposal_id = p.id AND a.revision_id = p.head_revision_id
		  AND a.reviewer_id = $2 AND a.revoked_at IS NULL
	)
))`

func (s *ContentItemStore) ListChangeProposalHistory(ctx context.Context, input domain.ChangeProposalHistoryInput) (domain.ChangeProposalHistory, error) {
	const query = `
		SELECT p.author_id, r.id, r.parent_revision_id, r.ordinal, r.message, r.created_at
		FROM content_change_proposals AS p
		JOIN content_items AS i ON i.id = p.content_item_id
		LEFT JOIN LATERAL (
		    SELECT id, parent_revision_id, ordinal, message, created_at
		    FROM content_change_proposal_revisions
		    WHERE proposal_id = p.id AND ordinal <= $3
		    ORDER BY ordinal DESC LIMIT $4
		) AS r ON true
		WHERE p.id = $1 AND ` + proposalReader + ` ORDER BY r.ordinal DESC`
	var lastOrdinal int32 = math.MaxInt32
	if input.BeforeOrdinal != 0 {
		lastOrdinal = input.BeforeOrdinal - 1
	}
	rows, err := s.db.Query(ctx, query, input.ProposalID, input.ActorID, lastOrdinal, input.PageSize+1)
	if err != nil {
		return domain.ChangeProposalHistory{}, err
	}
	defer rows.Close()

	page := domain.ChangeProposalHistory{Entries: make([]domain.ChangeProposalHistoryEntry, 0, input.PageSize)}
	found := false
	for rows.Next() {
		found = true
		var entry domain.ChangeProposalHistoryEntry
		var id, parentID, message *string
		var ordinal *int32
		var createdAt *time.Time
		if err = rows.Scan(&entry.AuthorID, &id, &parentID, &ordinal, &message, &createdAt); err != nil {
			return domain.ChangeProposalHistory{}, err
		}
		if id == nil {
			continue
		}
		if len(page.Entries) == int(input.PageSize) {
			page.NextBeforeOrdinal = page.Entries[len(page.Entries)-1].Ordinal

			break
		}
		entry.ID, entry.ParentRevisionID, entry.Message = *id, stringFromPtr(parentID), *message
		entry.Ordinal, entry.CreatedAt = *ordinal, *createdAt
		page.Entries = append(page.Entries, entry)
	}
	if err = rows.Err(); err != nil {
		return domain.ChangeProposalHistory{}, err
	}
	if !found {
		return domain.ChangeProposalHistory{}, service.ErrProposalNotFound
	}

	return page, nil
}

func (s *ContentItemStore) GetChangeProposalDiffSources(ctx context.Context, input domain.ChangeProposalDiffInput) (domain.ChangeProposalDiffSources, error) {
	const query = `
		SELECT b.id, r.id,
		       CASE WHEN octet_length(b.source) <= $4 THEN b.source END,
		       CASE WHEN octet_length(r.source) <= $4 THEN r.source END
		FROM content_change_proposals AS p
		JOIN content_items AS i ON i.id = p.content_item_id
		LEFT JOIN content_revisions AS b ON b.id = p.base_revision_id AND b.content_item_id = p.content_item_id
		LEFT JOIN content_change_proposal_revisions AS r ON r.proposal_id = p.id AND r.id = COALESCE($3, p.head_revision_id)
		WHERE p.id = $1 AND ` + proposalReader
	var baseID, revisionID, base, source *string
	err := s.db.QueryRow(ctx, query, input.ProposalID, input.ActorID, nullableString(input.RevisionID), validator.MaxSourceBytes).
		Scan(&baseID, &revisionID, &base, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChangeProposalDiffSources{}, service.ErrProposalNotFound
	}
	if err != nil {
		return domain.ChangeProposalDiffSources{}, err
	}
	if baseID == nil || revisionID == nil {
		return domain.ChangeProposalDiffSources{}, service.ErrProposalRevisionNotFound
	}

	return domain.ChangeProposalDiffSources{
		BaseRevisionID: *baseID, RevisionID: *revisionID,
		BaseSource: stringFromPtr(base), Source: stringFromPtr(source), Oversized: base == nil || source == nil,
	}, nil
}
