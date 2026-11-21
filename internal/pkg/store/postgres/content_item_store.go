package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/validator"
)

// Имена событий контента, публикуемых в outbox (совместимы с прежним publisher).
const (
	eventContentItemCreated     = "content_item.created"
	eventContentItemUpdated     = "content_item.updated"
	eventContentItemDeleted     = "content_item.deleted"
	eventContentRevisionCreated = "content_revision.created"
)

// contentItemEvent — конверт события контент-айтема для outbox (совместим с прежним publisher).
type contentItemEvent struct {
	Event      string             `json:"event"`
	OccurredAt time.Time          `json:"occurred_at"`
	Item       domain.ContentItem `json:"item"`
}

// contentRevisionEvent — конверт события ревизии контента для outbox.
type contentRevisionEvent struct {
	Event      string                 `json:"event"`
	OccurredAt time.Time              `json:"occurred_at"`
	Revision   domain.ContentRevision `json:"revision"`
}

type ContentItemStore struct {
	db            *pgxpool.Pool
	outboxTopic   string
	outboxEnabled bool
}

// StoreOption настраивает хранилище (например, атомарный outbox событий).
type StoreOption func(*ContentItemStore)

// WithOutbox включает постановку событий контента в transaction outbox (таблица outbox,
// DDL parker.OutboxSchema) в той же транзакции, что и бизнес-изменение: событие никогда
// не публикуется отдельно от коммита владеющей транзакции.
func WithOutbox(enabled bool, topic string) StoreOption {
	return func(s *ContentItemStore) {
		s.outboxEnabled = enabled
		s.outboxTopic = topic
	}
}

func NewContentItemStore(db *pgxpool.Pool, opts ...StoreOption) *ContentItemStore {
	store := &ContentItemStore{db: db}
	for _, opt := range opts {
		opt(store)
	}

	return store
}

type scanner interface {
	Scan(dest ...any) error
}

// execer — общий интерфейс Exec для pgx.Tx и пула (используется outbox-хелперами).
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
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
		VALUES ($1, $2, 'draft', $3, $4, $5, $6, $6)
	`

	_, err = tx.Exec(
		ctx,
		insertItem,
		itemID,
		input.Type,
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
		    updated_at = NOW()
		WHERE id = $1
	`

	if _, err = tx.Exec(ctx, updateCurrentRevision, itemID, revisionID); err != nil {
		return domain.ContentItem{}, err
	}

	// Событие пишется в outbox атомарно с созданием контент-айтема.
	item, err := s.get(ctx, tx, itemID, input.CreatedBy)
	if err != nil {
		return domain.ContentItem{}, err
	}
	if err = s.enqueueContentItem(ctx, tx, eventContentItemCreated, item); err != nil {
		return domain.ContentItem{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return domain.ContentItem{}, err
	}

	return item, nil
}

// enqueueContentItem пишет событие контент-айтема в outbox в текущей транзакции.
// Ошибка записи отменяет бизнес-изменение (атомарность publisher-гарантии).
func (s *ContentItemStore) enqueueContentItem(ctx context.Context, execer execer, eventName string, item domain.ContentItem) error {
	if !s.outboxEnabled || s.outboxTopic == "" {
		return nil
	}
	payload, err := json.Marshal(contentItemEvent{Event: eventName, OccurredAt: time.Now().UTC(), Item: item})
	if err != nil {
		return fmt.Errorf("marshal content item event: %w", err)
	}
	if _, err = execer.Exec(ctx, `
		INSERT INTO outbox (topic, message_key, message_value)
		VALUES ($1, $2, $3)`, s.outboxTopic, item.ID, payload); err != nil {
		return fmt.Errorf("insert content item event into outbox: %w", err)
	}

	return nil
}

// enqueueContentRevision пишет событие ревизии контента в outbox в текущей транзакции.
func (s *ContentItemStore) enqueueContentRevision(ctx context.Context, execer execer, revision domain.ContentRevision) error {
	if !s.outboxEnabled || s.outboxTopic == "" {
		return nil
	}
	payload, err := json.Marshal(contentRevisionEvent{Event: eventContentRevisionCreated, OccurredAt: time.Now().UTC(), Revision: revision})
	if err != nil {
		return fmt.Errorf("marshal content revision event: %w", err)
	}
	if _, err = execer.Exec(ctx, `
		INSERT INTO outbox (topic, message_key, message_value)
		VALUES ($1, $2, $3)`, s.outboxTopic, revision.ContentItemID, payload); err != nil {
		return fmt.Errorf("insert content revision event into outbox: %w", err)
	}

	return nil
}

func (s *ContentItemStore) Get(ctx context.Context, id, actorID string) (domain.ContentItem, error) {
	return s.get(ctx, s.db, id, actorID)
}

const visibleContentItems = `
		SELECT
			i.id,
			i.type,
			i.status,
			i.title,
			i.slug,
			i.description,
			r.id,
			i.published_revision_id,
			i.created_by,
			CASE WHEN i.created_by = $1 THEN i.updated_by ELSE r.created_by END,
			i.created_at,
			CASE WHEN i.created_by = $1 THEN i.updated_at
			     ELSE COALESCE(GREATEST(i.published_at, r.created_at), i.created_at) END,
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
		LEFT JOIN content_revisions r ON r.content_item_id = i.id
		    AND r.id = CASE WHEN i.created_by = $1 THEN i.current_revision_id ELSE i.published_revision_id END
		WHERE (i.created_by = $1 OR (i.status = 'published' AND r.id IS NOT NULL))
	`

func (s *ContentItemStore) List(ctx context.Context, actorID string) ([]domain.ContentItem, error) {
	rows, err := s.db.Query(ctx, visibleContentItems+` ORDER BY i.created_at DESC`, nullableString(actorID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.ContentItem, 0)
	for rows.Next() {
		item, scanErr := scanContentItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	for i := range items {
		if err = s.loadItemRelations(ctx, s.db, &items[i], actorID); err != nil {
			return nil, err
		}
	}

	return items, nil
}

func (s *ContentItemStore) Update(ctx context.Context, input domain.UpdateContentItemInput) (domain.ContentItem, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ContentItem{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	const query = `
		UPDATE content_items
		SET type = $2,
		    title = $3,
		    slug = $4,
		    description = $5,
		    updated_by = $6,
		    updated_at = NOW()
		WHERE id = $1 AND created_by = $6 AND status = 'draft'
	`

	tag, err := tx.Exec(ctx, query, input.ID, input.Type, input.Title, input.Slug, input.Description, nullableString(input.UpdatedBy))
	if err != nil {
		return domain.ContentItem{}, err
	}
	if tag.RowsAffected() == 0 {
		if _, err = lockOwnedContentItem(ctx, tx, input.ID, input.UpdatedBy); err != nil {
			return domain.ContentItem{}, err
		}

		return domain.ContentItem{}, service.ErrInvalidState
	}

	// Событие пишется в outbox атомарно с обновлением айтема.
	item, err := s.get(ctx, tx, input.ID, input.UpdatedBy)
	if err != nil {
		return domain.ContentItem{}, err
	}
	if err = s.enqueueContentItem(ctx, tx, eventContentItemUpdated, item); err != nil {
		return domain.ContentItem{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ContentItem{}, err
	}

	return item, nil
}

func (s *ContentItemStore) ChangeState(ctx context.Context, input domain.ChangeContentItemStateInput, target domain.ContentStatus) (domain.ContentItem, error) {
	if target != domain.ContentStatusPublished && target != domain.ContentStatusArchived {
		return domain.ContentItem{}, service.ErrInvalidState
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ContentItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := lockOwnedContentItem(ctx, tx, input.ID, input.ActorID)
	if err != nil {
		return domain.ContentItem{}, err
	}
	if current.status == domain.ContentStatusArchived && target != current.status {
		return domain.ContentItem{}, service.ErrInvalidState
	}
	if err = current.checkRevision(input.ExpectedRevisionID); err != nil {
		return domain.ContentItem{}, err
	}
	if target == domain.ContentStatusPublished {
		if err = validatePublication(ctx, tx, input.ID, current.revisionID); err != nil {
			return domain.ContentItem{}, err
		}
	}
	if target == current.status && (target == domain.ContentStatusArchived || current.publishedRevisionID == current.revisionID) {
		return s.get(ctx, tx, input.ID, input.ActorID)
	}

	const query = `
		UPDATE content_items
		SET status = $2,
		    published_revision_id = CASE WHEN $2 = 'published' THEN current_revision_id ELSE published_revision_id END,
		    published_at = CASE WHEN $2 = 'published' THEN NOW() ELSE published_at END,
		    archived_at = CASE WHEN $2 = 'archived' THEN NOW() ELSE NULL END,
		    updated_by = $3,
		    updated_at = NOW()
		WHERE id = $1
	`
	if _, err = tx.Exec(ctx, query, input.ID, target, input.ActorID); err != nil {
		return domain.ContentItem{}, err
	}
	item, err := s.get(ctx, tx, input.ID, input.ActorID)
	if err != nil {
		return domain.ContentItem{}, err
	}
	if err = s.enqueueContentItem(ctx, tx, eventContentItemUpdated, item); err != nil {
		return domain.ContentItem{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ContentItem{}, err
	}

	return item, nil
}

func validatePublication(ctx context.Context, tx pgx.Tx, itemID, revisionID string) error {
	const query = `
		SELECT r.format,
		       CASE WHEN r.format = 'markdown' AND octet_length(r.source) <= $3 THEN r.source END,
		       EXISTS (
		           SELECT 1 FROM content_assets
		           WHERE content_item_id = $1 AND revision_id = $2
		           OFFSET $4
		       )
		FROM content_revisions r
		WHERE r.content_item_id = $1 AND r.id = $2
	`
	var format domain.ContentFormat
	var source *string
	var tooManyAssets bool
	err := tx.QueryRow(ctx, query, itemID, revisionID, validator.MaxSourceBytes, validator.MaxAssets).
		Scan(&format, &source, &tooManyAssets)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrInvalidState
	}
	if err != nil {
		return err
	}
	if format == domain.ContentFormatMarkdown && source == nil {
		return validator.ValidateSourceSize(validator.MaxSourceBytes + 1)
	}
	if tooManyAssets {
		return validator.ValidateAssetCount(validator.MaxAssets + 1)
	}

	return validator.ValidatePublishableSource(format, stringFromPtr(source))
}

func (s *ContentItemStore) Delete(ctx context.Context, id, actorID string) (domain.ContentItem, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.ContentItem{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if _, err = lockOwnedContentItem(ctx, tx, id, actorID); err != nil {
		return domain.ContentItem{}, err
	}
	item, err := s.get(ctx, tx, id, actorID)
	if err != nil {
		return domain.ContentItem{}, err
	}

	const query = `
		DELETE FROM content_items
		WHERE id = $1
	`

	tag, err := tx.Exec(ctx, query, id)
	if err != nil {
		return domain.ContentItem{}, err
	}
	if tag.RowsAffected() != 1 {
		return domain.ContentItem{}, service.ErrNotFound
	}

	// Событие пишется в outbox атомарно с удалением айтема.
	if err = s.enqueueContentItem(ctx, tx, eventContentItemDeleted, item); err != nil {
		return domain.ContentItem{}, err
	}
	if err = tx.Commit(ctx); err != nil {
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

	itemID := input.ContentItemID
	current, err := lockOwnedContentItem(ctx, tx, itemID, input.CreatedBy)
	if err != nil {
		return domain.ContentRevision{}, err
	}
	if current.status == domain.ContentStatusArchived {
		return domain.ContentRevision{}, service.ErrInvalidState
	}
	if err = current.checkRevision(input.ExpectedRevisionID); err != nil {
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

	// Событие пишется в outbox атомарно с созданием ревизии.
	if err = s.enqueueContentRevision(ctx, tx, revision); err != nil {
		return domain.ContentRevision{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ContentRevision{}, err
	}

	return revision, nil
}

func (s *ContentItemStore) ListRevisions(ctx context.Context, contentItemID, actorID string) ([]domain.ContentRevision, error) {
	const query = `
		SELECT id, content_item_id, revision, format, source, source_hash, message, created_by, created_at
		FROM content_revisions
		WHERE content_item_id = $1
		  AND EXISTS (SELECT 1 FROM content_items WHERE id = $1 AND created_by = $2)
		ORDER BY revision DESC
	`

	rows, err := s.db.Query(ctx, query, contentItemID, nullableString(actorID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	revisions := make([]domain.ContentRevision, 0)
	for rows.Next() {
		revision, scanErr := scanContentRevision(rows)
		if scanErr != nil {
			return nil, scanErr
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
			WHERE id = $1 AND created_by = $2
		)
	`
	if err = s.db.QueryRow(ctx, existsQuery, contentItemID, nullableString(actorID)).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, service.ErrNotFound
	}

	return revisions, nil
}

type lockedContentItem struct {
	status              domain.ContentStatus
	revisionID          string
	publishedRevisionID string
}

func (item lockedContentItem) checkRevision(expected string) error {
	if item.revisionID == "" || item.revisionID != expected {
		return service.ErrRevisionConflict
	}

	return nil
}

func lockOwnedContentItem(ctx context.Context, tx pgx.Tx, id, actorID string) (lockedContentItem, error) {
	const query = `
		SELECT status, current_revision_id, published_revision_id
		FROM content_items
		WHERE id = $1 AND created_by = $2
		FOR UPDATE
	`
	var item lockedContentItem
	var current, published *string
	err := tx.QueryRow(ctx, query, id, nullableString(actorID)).Scan(&item.status, &current, &published)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedContentItem{}, service.ErrNotFound
	}
	if err != nil {
		return lockedContentItem{}, err
	}
	item.revisionID = stringFromPtr(current)
	item.publishedRevisionID = stringFromPtr(published)

	return item, nil
}

func (s *ContentItemStore) get(ctx context.Context, queryer queryer, id, actorID string) (domain.ContentItem, error) {
	const query = visibleContentItems + ` AND i.id = $2`
	item, err := scanContentItem(queryer.QueryRow(ctx, query, nullableString(actorID), id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ContentItem{}, service.ErrNotFound
	}
	if err != nil {
		return domain.ContentItem{}, err
	}
	if err = s.loadItemRelations(ctx, queryer, &item, actorID); err != nil {
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

func (s *ContentItemStore) loadItemRelations(ctx context.Context, queryer queryer, item *domain.ContentItem, actorID string) error {
	tags, err := s.listTags(ctx, queryer, item.ID)
	if err != nil {
		return err
	}
	owner := actorID != "" && item.CreatedBy == actorID
	assets, err := s.listAssets(ctx, queryer, item.ID, item.CurrentRevisionID, owner)
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

func (s *ContentItemStore) listAssets(ctx context.Context, queryer queryer, contentItemID, revisionID string, owner bool) ([]domain.ContentAsset, error) {
	const query = `
		SELECT id, content_item_id, revision_id, asset_id, kind, title, position, created_at
		FROM content_assets
		WHERE content_item_id = $1
		  AND ($3::boolean OR revision_id = $2)
		ORDER BY position, id
	`

	rows, err := queryer.Query(ctx, query, contentItemID, nullableString(revisionID), owner)
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
