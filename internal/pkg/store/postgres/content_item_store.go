package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

type ContentItemStore struct {
	db *pgxpool.Pool
}

type scanner interface {
	Scan(dest ...any) error
}

func NewContentItemStore(db *pgxpool.Pool) *ContentItemStore {
	return &ContentItemStore{db: db}
}

func (s *ContentItemStore) PingContext(ctx context.Context) error {
	return s.db.Ping(ctx)
}

func (s *ContentItemStore) Create(ctx context.Context, input domain.CreateContentItemInput) (domain.ContentItem, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ContentItem{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	itemID := uuid.NewString()
	revisionID := uuid.NewString()

	const insertItem = `
		INSERT INTO content_items (
			id,
			type,
			status,
			title,
			slug,
			description,
			created_by,
			updated_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
	`

	_, err = tx.Exec(
		ctx,
		insertItem,
		itemID,
		input.Type,
		input.Status,
		input.Title,
		input.Slug,
		input.Description,
		nullableString(input.CreatedBy),
	)
	if err != nil {
		return domain.ContentItem{}, err
	}

	const insertRevision = `
		INSERT INTO content_revisions (
			id,
			content_item_id,
			revision,
			format,
			source,
			source_hash,
			message,
			created_by
		)
		VALUES ($1, $2, 1, $3, $4, $5, $6, $7)
	`

	_, err = tx.Exec(
		ctx,
		insertRevision,
		revisionID,
		itemID,
		input.Format,
		input.Source,
		hashSource(input.Source),
		input.Message,
		nullableString(input.CreatedBy),
	)
	if err != nil {
		return domain.ContentItem{}, err
	}

	if err = s.attachTags(ctx, tx, itemID, input.Tags); err != nil {
		return domain.ContentItem{}, err
	}
	if err = s.attachAssets(ctx, tx, itemID, revisionID, input.Assets); err != nil {
		return domain.ContentItem{}, err
	}

	const updateCurrentRevision = `
		UPDATE content_items
		SET current_revision_id = $2,
		    published_revision_id = CASE WHEN status = 'published' THEN $2 ELSE published_revision_id END,
		    published_at = CASE WHEN status = 'published' THEN NOW() ELSE published_at END,
		    updated_at = NOW()
		WHERE id = $1
	`

	if _, err = tx.Exec(ctx, updateCurrentRevision, itemID, revisionID); err != nil {
		return domain.ContentItem{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return domain.ContentItem{}, err
	}

	return s.Get(ctx, itemID)
}

func (s *ContentItemStore) Get(ctx context.Context, id string) (domain.ContentItem, error) {
	item, err := s.get(ctx, s.db, id)
	if err != nil {
		return domain.ContentItem{}, err
	}

	return item, nil
}

func (s *ContentItemStore) List(ctx context.Context) ([]domain.ContentItem, error) {
	const query = `
		SELECT
			i.id,
			i.type,
			i.status,
			i.title,
			i.slug,
			i.description,
			i.current_revision_id,
			i.published_revision_id,
			i.created_by,
			i.updated_by,
			i.created_at,
			i.updated_at,
			i.published_at,
			i.archived_at,
			r.id,
			r.content_item_id,
			r.revision,
			r.format,
			r.source,
			r.source_hash,
			r.message,
			r.created_by,
			r.created_at
		FROM content_items i
		LEFT JOIN content_revisions r ON r.id = i.current_revision_id
		ORDER BY i.created_at DESC
	`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.ContentItem, 0)
	for rows.Next() {
		item, err := scanContentItem(rows)
		if err != nil {
			return nil, err
		}
		if err = s.loadItemRelations(ctx, s.db, &item); err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *ContentItemStore) Update(ctx context.Context, input domain.UpdateContentItemInput) (domain.ContentItem, error) {
	const query = `
		UPDATE content_items
		SET type = $2,
		    status = $3,
		    title = $4,
		    slug = $5,
		    description = $6,
		    updated_by = $7,
		    updated_at = NOW(),
		    published_revision_id = CASE
		        WHEN $3 = 'published' THEN current_revision_id
		        WHEN $3 = 'draft' THEN NULL
		        ELSE published_revision_id
		    END,
		    published_at = CASE
		        WHEN $3 = 'published' THEN COALESCE(published_at, NOW())
		        WHEN $3 = 'draft' THEN NULL
		        ELSE published_at
		    END,
		    archived_at = CASE
		        WHEN $3 = 'archived' THEN COALESCE(archived_at, NOW())
		        WHEN $3 IN ('draft', 'published') THEN NULL
		        ELSE archived_at
		    END
		WHERE id = $1
		RETURNING id
	`

	var id string
	err := s.db.QueryRow(
		ctx,
		query,
		input.ID,
		input.Type,
		input.Status,
		input.Title,
		input.Slug,
		input.Description,
		nullableString(input.UpdatedBy),
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ContentItem{}, service.ErrNotFound
	}
	if err != nil {
		return domain.ContentItem{}, err
	}

	return s.Get(ctx, id)
}

func (s *ContentItemStore) Delete(ctx context.Context, id string) (domain.ContentItem, error) {
	item, err := s.Get(ctx, id)
	if err != nil {
		return domain.ContentItem{}, err
	}

	const query = `
		DELETE FROM content_items
		WHERE id = $1
	`

	if _, err = s.db.Exec(ctx, query, id); err != nil {
		return domain.ContentItem{}, err
	}

	return item, nil
}

func (s *ContentItemStore) CreateRevision(ctx context.Context, input domain.CreateContentRevisionInput) (domain.ContentRevision, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ContentRevision{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const lockItem = `
		SELECT id
		FROM content_items
		WHERE id = $1
		FOR UPDATE
	`

	var itemID string
	err = tx.QueryRow(ctx, lockItem, input.ContentItemID).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ContentRevision{}, service.ErrNotFound
	}
	if err != nil {
		return domain.ContentRevision{}, err
	}

	const nextRevision = `
		SELECT COALESCE(MAX(revision), 0) + 1
		FROM content_revisions
		WHERE content_item_id = $1
	`

	var revisionNumber int
	if err = tx.QueryRow(ctx, nextRevision, itemID).Scan(&revisionNumber); err != nil {
		return domain.ContentRevision{}, err
	}

	revisionID := uuid.NewString()
	const insertRevision = `
		INSERT INTO content_revisions (
			id,
			content_item_id,
			revision,
			format,
			source,
			source_hash,
			message,
			created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, content_item_id, revision, format, source, source_hash, message, created_by, created_at
	`

	revision, err := scanContentRevision(tx.QueryRow(
		ctx,
		insertRevision,
		revisionID,
		itemID,
		revisionNumber,
		input.Format,
		input.Source,
		hashSource(input.Source),
		input.Message,
		nullableString(input.CreatedBy),
	))
	if err != nil {
		return domain.ContentRevision{}, err
	}

	const updateItem = `
		UPDATE content_items
		SET current_revision_id = $2,
		    updated_by = $3,
		    updated_at = NOW()
		WHERE id = $1
	`

	if _, err = tx.Exec(ctx, updateItem, itemID, revision.ID, nullableString(input.CreatedBy)); err != nil {
		return domain.ContentRevision{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return domain.ContentRevision{}, err
	}

	return revision, nil
}

func (s *ContentItemStore) ListRevisions(ctx context.Context, contentItemID string) ([]domain.ContentRevision, error) {
	const query = `
		SELECT id, content_item_id, revision, format, source, source_hash, message, created_by, created_at
		FROM content_revisions
		WHERE content_item_id = $1
		ORDER BY revision DESC
	`

	rows, err := s.db.Query(ctx, query, contentItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	revisions := make([]domain.ContentRevision, 0)
	for rows.Next() {
		revision, err := scanContentRevision(rows)
		if err != nil {
			return nil, err
		}

		revisions = append(revisions, revision)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(revisions) > 0 {
		return revisions, nil
	}

	var exists bool
	const existsQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM content_items
			WHERE id = $1
		)
	`
	if err = s.db.QueryRow(ctx, existsQuery, contentItemID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, service.ErrNotFound
	}

	return revisions, nil
}

func (s *ContentItemStore) get(ctx context.Context, queryer queryer, id string) (domain.ContentItem, error) {
	const query = `
		SELECT
			i.id,
			i.type,
			i.status,
			i.title,
			i.slug,
			i.description,
			i.current_revision_id,
			i.published_revision_id,
			i.created_by,
			i.updated_by,
			i.created_at,
			i.updated_at,
			i.published_at,
			i.archived_at,
			r.id,
			r.content_item_id,
			r.revision,
			r.format,
			r.source,
			r.source_hash,
			r.message,
			r.created_by,
			r.created_at
		FROM content_items i
		LEFT JOIN content_revisions r ON r.id = i.current_revision_id
		WHERE i.id = $1
	`

	item, err := scanContentItem(queryer.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ContentItem{}, service.ErrNotFound
	}
	if err != nil {
		return domain.ContentItem{}, err
	}
	if err = s.loadItemRelations(ctx, queryer, &item); err != nil {
		return domain.ContentItem{}, err
	}

	return item, nil
}

func (s *ContentItemStore) attachTags(ctx context.Context, tx pgx.Tx, contentItemID string, tags []string) error {
	const upsertTag = `
		INSERT INTO tags (id, name, slug)
		VALUES ($1, $2, $3)
		ON CONFLICT (slug) DO UPDATE
		SET name = EXCLUDED.name
		RETURNING id
	`
	const attachTag = `
		INSERT INTO content_tags (content_item_id, tag_id)
		VALUES ($1, $2)
		ON CONFLICT (content_item_id, tag_id) DO NOTHING
	`

	for _, tag := range tags {
		slug := tagSlug(tag)
		tagID := uuid.NewString()
		if err := tx.QueryRow(ctx, upsertTag, tagID, tag, slug).Scan(&tagID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, attachTag, contentItemID, tagID); err != nil {
			return err
		}
	}

	return nil
}

func (s *ContentItemStore) attachAssets(ctx context.Context, tx pgx.Tx, contentItemID string, revisionID string, assets []domain.CreateContentAssetInput) error {
	const insertAsset = `
		INSERT INTO content_assets (
			id,
			content_item_id,
			revision_id,
			asset_id,
			kind,
			title,
			position
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	for _, asset := range assets {
		_, err := tx.Exec(
			ctx,
			insertAsset,
			uuid.NewString(),
			contentItemID,
			nullableString(revisionID),
			asset.AssetID,
			asset.Kind,
			asset.Title,
			asset.Position,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *ContentItemStore) loadItemRelations(ctx context.Context, queryer queryer, item *domain.ContentItem) error {
	tags, err := s.listTags(ctx, queryer, item.ID)
	if err != nil {
		return err
	}
	assets, err := s.listAssets(ctx, queryer, item.ID)
	if err != nil {
		return err
	}

	item.Tags = tags
	item.Assets = assets

	return nil
}

func (s *ContentItemStore) listTags(ctx context.Context, queryer queryer, contentItemID string) ([]domain.Tag, error) {
	const query = `
		SELECT t.id, t.name, t.slug, t.created_at
		FROM tags t
		JOIN content_tags ct ON ct.tag_id = t.id
		WHERE ct.content_item_id = $1
		ORDER BY t.name
	`

	rows, err := queryer.Query(ctx, query, contentItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make([]domain.Tag, 0)
	for rows.Next() {
		var tag domain.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Slug, &tag.CreatedAt); err != nil {
			return nil, err
		}

		tags = append(tags, tag)
	}

	return tags, rows.Err()
}

func (s *ContentItemStore) listAssets(ctx context.Context, queryer queryer, contentItemID string) ([]domain.ContentAsset, error) {
	const query = `
		SELECT id, content_item_id, revision_id, asset_id, kind, title, position, created_at
		FROM content_assets
		WHERE content_item_id = $1
		ORDER BY position, id
	`

	rows, err := queryer.Query(ctx, query, contentItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assets := make([]domain.ContentAsset, 0)
	for rows.Next() {
		asset, err := scanContentAsset(rows)
		if err != nil {
			return nil, err
		}

		assets = append(assets, asset)
	}

	return assets, rows.Err()
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func scanContentItem(row scanner) (domain.ContentItem, error) {
	var item domain.ContentItem
	var contentType string
	var status string
	var currentRevisionID *string
	var publishedRevisionID *string
	var createdBy *string
	var updatedBy *string
	var publishedAt *time.Time
	var archivedAt *time.Time
	var revision domain.ContentRevision
	var revisionID *string
	var revisionContentItemID *string
	var revisionNumber *int64
	var revisionFormat *string
	var revisionSource *string
	var revisionSourceHash *string
	var revisionMessage *string
	var revisionCreatedBy *string
	var revisionCreatedAt *time.Time

	err := row.Scan(
		&item.ID,
		&contentType,
		&status,
		&item.Title,
		&item.Slug,
		&item.Description,
		&currentRevisionID,
		&publishedRevisionID,
		&createdBy,
		&updatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
		&publishedAt,
		&archivedAt,
		&revisionID,
		&revisionContentItemID,
		&revisionNumber,
		&revisionFormat,
		&revisionSource,
		&revisionSourceHash,
		&revisionMessage,
		&revisionCreatedBy,
		&revisionCreatedAt,
	)
	if err != nil {
		return domain.ContentItem{}, err
	}

	item.Type = domain.ContentType(contentType)
	item.Status = domain.ContentStatus(status)
	item.CurrentRevisionID = stringFromPtr(currentRevisionID)
	item.PublishedRevisionID = stringFromPtr(publishedRevisionID)
	item.CreatedBy = stringFromPtr(createdBy)
	item.UpdatedBy = stringFromPtr(updatedBy)
	item.PublishedAt = publishedAt
	item.ArchivedAt = archivedAt

	if revisionID != nil {
		revision.ID = *revisionID
		revision.ContentItemID = *revisionContentItemID
		if revisionNumber != nil {
			revision.Revision = int(*revisionNumber)
		}
		revision.Format = domain.ContentFormat(stringFromPtr(revisionFormat))
		revision.Source = stringFromPtr(revisionSource)
		revision.SourceHash = stringFromPtr(revisionSourceHash)
		revision.Message = stringFromPtr(revisionMessage)
		revision.CreatedBy = stringFromPtr(revisionCreatedBy)
		if revisionCreatedAt != nil {
			revision.CreatedAt = *revisionCreatedAt
		}
		item.CurrentRevision = &revision
	}

	return item, nil
}

func scanContentRevision(row scanner) (domain.ContentRevision, error) {
	var revision domain.ContentRevision
	var format string
	var createdBy *string

	err := row.Scan(
		&revision.ID,
		&revision.ContentItemID,
		&revision.Revision,
		&format,
		&revision.Source,
		&revision.SourceHash,
		&revision.Message,
		&createdBy,
		&revision.CreatedAt,
	)
	if err != nil {
		return domain.ContentRevision{}, err
	}

	revision.Format = domain.ContentFormat(format)
	revision.CreatedBy = stringFromPtr(createdBy)

	return revision, nil
}

func scanContentAsset(row scanner) (domain.ContentAsset, error) {
	var asset domain.ContentAsset
	var revisionID *string
	var kind string

	err := row.Scan(
		&asset.ID,
		&asset.ContentItemID,
		&revisionID,
		&asset.AssetID,
		&kind,
		&asset.Title,
		&asset.Position,
		&asset.CreatedAt,
	)
	if err != nil {
		return domain.ContentAsset{}, err
	}

	asset.RevisionID = stringFromPtr(revisionID)
	asset.Kind = domain.AssetKind(kind)

	return asset, nil
}

func hashSource(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return value
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func tagSlug(tag string) string {
	return strings.Join(strings.Fields(strings.ToLower(tag)), "-")
}
