package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
)

const catalogTopicIDs = `ARRAY(
	SELECT t.topic_id::text FROM content_item_topics AS t WHERE t.content_item_id = i.id ORDER BY t.topic_id
)`

const replaceContentTopics = `
	removed AS (
	    DELETE FROM content_item_topics
	    WHERE content_item_id = $1 AND NOT (topic_id = ANY(COALESCE($2::uuid[], '{}')))
	)
	INSERT INTO content_item_topics (content_item_id, topic_id)
	SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING
`

func writeLearningMetadata(ctx context.Context, tx pgx.Tx, id string, learning *domain.LearningMetadata) error {
	const query = `
		WITH metadata AS (
		    UPDATE content_items SET estimated_minutes = $3, learning_outcomes = COALESCE($4::text[], '{}')
		    WHERE id = $1
		), ` + replaceContentTopics
	_, err := tx.Exec(ctx, query, id, learning.TopicIDs, learning.EstimatedMinutes, learning.LearningOutcomes)
	return err
}

func (s *ContentItemStore) ListCatalog(ctx context.Context, filter domain.CatalogFilter, cursor *domain.CatalogCursor) ([]domain.CatalogEntry, error) {
	query, args := catalogQuery(filter, cursor)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.CatalogEntry, error) {
		var entry domain.CatalogEntry
		var author, revision *string
		err := row.Scan(&entry.ID, &entry.Title, &entry.Slug, &entry.Status, &author, &entry.CreatedAt, &revision,
			&entry.Learning.EstimatedMinutes, &entry.Learning.LearningOutcomes, &entry.Learning.TopicIDs)
		entry.AuthorID, entry.RevisionID = stringFromPtr(author), stringFromPtr(revision)
		return entry, err
	})
}

func catalogQuery(filter domain.CatalogFilter, cursor *domain.CatalogCursor) (string, []any) {
	query := `
		SELECT i.id, i.title, i.slug, i.status, i.created_by, i.created_at,
		       CASE WHEN i.status = 'published' THEN i.published_revision_id ELSE i.current_revision_id END,
		       i.estimated_minutes, i.learning_outcomes, ` + catalogTopicIDs + `
		FROM content_items AS i
		WHERE NOT i.moderation_hidden AND ((i.status <> 'published' AND i.created_by = $1)
		    OR (i.status = 'published' AND EXISTS (
		        SELECT 1 FROM content_revisions AS r WHERE r.id = i.published_revision_id AND r.content_item_id = i.id
		    )))`
	args := []any{nullableString(filter.ActorID)}
	if filter.TopicID != "" {
		args = append(args, filter.TopicID)
		query += fmt.Sprintf(` AND EXISTS (SELECT 1 FROM content_item_topics AS b WHERE b.content_item_id = i.id AND b.topic_id = $%d)`, len(args))
	}
	if filter.AuthorID != "" {
		args = append(args, filter.AuthorID)
		query += fmt.Sprintf(` AND i.created_by = $%d`, len(args))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		query += fmt.Sprintf(` AND i.status = $%d`, len(args))
	}
	if cursor != nil {
		args = append(args, cursor.CreatedAt, cursor.ID)
		query += fmt.Sprintf(` AND (i.created_at, i.id) < ($%d, $%d)`, len(args)-1, len(args))
	}
	args = append(args, filter.PageSize+1)
	query += fmt.Sprintf(` ORDER BY i.created_at DESC, i.id DESC LIMIT $%d`, len(args))
	return query, args
}
