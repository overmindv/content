package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/validator"
	"github.com/overmindv/content/pkg/api/activity"
)

type activityTopics struct {
	material     string
	learning     string
	contribution string
}

func WithMaterialActivity(material, learning string) StoreOption {
	return func(s *ContentItemStore) {
		s.activity.material, s.activity.learning = material, learning
	}
}

func WithEditorialActivity(topic string) StoreOption {
	return func(s *ContentItemStore) {
		s.activity.contribution = topic
	}
}

func (s *ContentItemStore) recordMaterialState(ctx context.Context, tx pgx.Tx, item domain.ContentItem, tombstone bool) (int64, error) {
	kind := activity.MaterialPublished
	if tombstone {
		kind = activity.MaterialArchived
	}

	return s.recordMaterialEvent(ctx, tx, item, kind, activity.MaterialState{Tombstone: tombstone})
}

func (s *ContentItemStore) recordMaterialEvent(
	ctx context.Context, tx pgx.Tx, item domain.ContentItem, kind string, state activity.MaterialState,
) (int64, error) {
	var metadata []byte
	if !state.Tombstone {
		state.Metadata = &activity.ResourceMetadata{Title: resourceTitle(item.Title), Type: string(item.Type), TopicIDs: []string{}}
		if item.CurrentRevision != nil {
			state.Metadata.Format = string(item.CurrentRevision.Format)
		}
		if item.Learning != nil {
			state.Metadata.TopicIDs = append(state.Metadata.TopicIDs, item.Learning.TopicIDs...)
			state.Metadata.EstimatedMinutes = item.Learning.EstimatedMinutes
		}
		var err error
		metadata, err = json.Marshal(state.Metadata)
		if err != nil {
			return 0, err
		}
	}

	// Advancing the clock in this transaction keeps the snapshot watermark commit-ordered.
	const record = `
		WITH next AS (
		    UPDATE content_catalog_clock SET watermark = watermark + 1 WHERE singleton RETURNING watermark
		), material AS (
		    INSERT INTO content_catalog_materials (material_id, first_version)
		    SELECT $1, watermark FROM next ON CONFLICT DO NOTHING
		)
		INSERT INTO content_catalog_versions (material_id, version, revision_id, tombstone, metadata)
		SELECT $1, watermark, $2, $3, $4::jsonb FROM next RETURNING version`
	var version int64
	if err := tx.QueryRow(ctx, record, item.ID, item.PublishedRevisionID, state.Tombstone, metadata).Scan(&version); err != nil {
		return 0, fmt.Errorf("record material state: %w", err)
	}
	if s.activity.material == "" {
		return version, nil
	}

	event := newActivity(kind, item.ID, item.PublishedRevisionID, version, state)

	return version, s.enqueueActivity(ctx, tx, s.activity.material, event.EventID, event.AggregateID, event)
}

func resourceTitle(title string) string {
	title = strings.TrimSpace(title)
	size := 0
	for index := range title {
		if size == 256 {
			return title[:index]
		}
		size++
	}

	return title
}

func newActivity[T any](kind, materialID, revisionID string, version int64, payload T) activity.Event[T] {
	return activity.Event[T]{
		EventID: uuid.NewString(), EventType: kind, SchemaVersion: activity.SchemaVersion, OccurredAt: time.Now().UTC(),
		AggregateID: materialID, AggregateVersion: version, RevisionID: revisionID, Payload: payload,
	}
}

func (s *ContentItemStore) enqueueActivity(ctx context.Context, tx pgx.Tx, topic, eventID, key string, event any) error {
	if topic == "" {
		return nil
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO outbox (id, topic, message_key, message_value) VALUES ($1, $2, $3, $4)`,
		eventID, topic, key, payload); err != nil {
		return fmt.Errorf("enqueue material activity: %w", err)
	}

	return nil
}

func (s *ContentItemStore) SnapshotMaterials(ctx context.Context, input domain.MaterialSnapshotInput) (domain.MaterialSnapshot, error) {
	var watermark int64
	if err := s.db.QueryRow(ctx, `SELECT watermark FROM content_catalog_clock WHERE singleton`).Scan(&watermark); err != nil {
		return domain.MaterialSnapshot{}, err
	}
	if input.Watermark != nil {
		if *input.Watermark > watermark {
			return domain.MaterialSnapshot{}, validator.Error{Message: "watermark has not been issued"}
		}
		watermark = *input.Watermark
	}
	const query = `
		WITH page AS (
		    SELECT material_id FROM content_catalog_materials
		    WHERE material_id > $1 AND first_version <= $2 ORDER BY material_id LIMIT $3
		)
		SELECT p.material_id, v.revision_id, v.version, v.tombstone, v.metadata
		FROM page AS p JOIN LATERAL (
		    SELECT revision_id, version, tombstone, metadata FROM content_catalog_versions
		    WHERE material_id = p.material_id AND version <= $2 ORDER BY version DESC LIMIT 1
		) AS v ON true ORDER BY p.material_id`
	after := input.AfterID
	if after == "" {
		after = uuid.Nil.String()
	}
	rows, err := s.db.Query(ctx, query, after, watermark, input.PageSize+1)
	if err != nil {
		return domain.MaterialSnapshot{}, err
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.MaterialVersion, error) {
		var entry domain.MaterialVersion
		scanErr := row.Scan(&entry.MaterialID, &entry.RevisionID, &entry.Version, &entry.State.Tombstone, &entry.State.Metadata)

		return entry, scanErr
	})
	if err != nil {
		return domain.MaterialSnapshot{}, err
	}
	page := domain.MaterialSnapshot{Watermark: watermark, Entries: entries}
	if len(entries) > int(input.PageSize) {
		page.Entries = entries[:input.PageSize]
		page.NextAfterID = page.Entries[len(page.Entries)-1].MaterialID
	}

	return page, nil
}

func (s *ContentItemStore) ConfirmMaterialRead(ctx context.Context, input domain.ConfirmMaterialReadInput) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		const lock = `
			SELECT published_revision_id FROM content_items
			WHERE id = $1 AND status = 'published' AND NOT moderation_hidden AND published_revision_id IS NOT NULL FOR SHARE`
		var revision string
		err := tx.QueryRow(ctx, lock, input.MaterialID).Scan(&revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return service.ErrNotFound
		}
		if err != nil {
			return err
		}
		if input.RevisionID != revision {
			return service.ErrRevisionConflict
		}
		const insert = `
			INSERT INTO content_read_confirmations (material_id, revision_id, user_id)
			VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`
		tag, err := tx.Exec(ctx, insert, input.MaterialID, input.RevisionID, input.ActorID)
		if err != nil || tag.RowsAffected() == 0 || s.activity.learning == "" {
			return err
		}
		const latest = `SELECT version FROM content_catalog_versions WHERE material_id = $1 ORDER BY version DESC LIMIT 1`
		var version int64
		if err = tx.QueryRow(ctx, latest, input.MaterialID).Scan(&version); err != nil {
			return err
		}
		event := newActivity(activity.MaterialReadConfirmed, input.MaterialID, input.RevisionID, version,
			activity.ReadConfirmation{UserID: input.ActorID, Evidence: activity.SelfReported})

		return s.enqueueActivity(ctx, tx, s.activity.learning, event.EventID, event.AggregateID, event)
	})
}
