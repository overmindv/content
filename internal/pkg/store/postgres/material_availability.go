package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
)

func (s *ContentItemStore) AvailableMaterialVersions(ctx context.Context, keys []domain.MaterialVersionKey) ([]domain.MaterialVersionKey, error) {
	materialIDs := make([]string, len(keys))
	revisionIDs := make([]string, len(keys))
	for i, key := range keys {
		materialIDs[i], revisionIDs[i] = key.MaterialID, key.RevisionID
	}
	const query = `
		SELECT i.id::text, r.id::text
		FROM unnest($1::uuid[], $2::uuid[]) WITH ORDINALITY AS wanted(material_id, revision_id, position)
		JOIN content_items AS i ON i.id = wanted.material_id AND i.published_revision_id = wanted.revision_id
		JOIN content_revisions AS r ON r.id = i.published_revision_id AND r.content_item_id = i.id
		WHERE i.status = 'published' AND NOT i.moderation_hidden
		ORDER BY wanted.position`
	rows, err := s.db.Query(ctx, query, materialIDs, revisionIDs)
	if err != nil {
		return nil, fmt.Errorf("query available material versions: %w", err)
	}
	available, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.MaterialVersionKey, error) {
		var key domain.MaterialVersionKey
		scanErr := row.Scan(&key.MaterialID, &key.RevisionID)

		return key, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("scan available material versions: %w", err)
	}

	return available, nil
}
