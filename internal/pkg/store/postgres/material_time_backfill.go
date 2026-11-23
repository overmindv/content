package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/pkg/api/activity"
)

const (
	MaxMaterialTimeBatch   = 50
	MaxMaterialTimeBatches = 128
	MaxMaterialTimeEvents  = 512
	materialEventSize      = 16 << 10
	materialTimeTimeout    = 10 * time.Second
	materialTimeSample     = 50
	materialTimeConflict   = "conflict"
	materialTimeInvalid    = "invalid_event"
	materialTimeLimit      = "event_limit"
)

type MaterialTimeCursor struct {
	MaterialID uuid.UUID
	Version    int64
}

type MaterialTimeOptions struct {
	Topic                          string
	After                          MaterialTimeCursor
	BatchSize, Batches, EventLimit int
	Apply                          bool
}

type MaterialTimeReport struct {
	Scanned             int64                 `json:"scanned"`
	Matched             int64                 `json:"matched"`
	Updated             int64                 `json:"updated"`
	Missing             int64                 `json:"missing"`
	Unsafe              int64                 `json:"unsafe"`
	Legacy              int64                 `json:"legacy"`
	Complete            bool                  `json:"complete"`
	NextAfter           *MaterialTimeCursor   `json:"-"`
	Unresolved          []MaterialTimeProblem `json:"unresolved,omitempty"`
	UnresolvedTruncated bool                  `json:"unresolved_truncated,omitempty"`
}

type MaterialTimeProblem struct {
	MaterialID uuid.UUID `json:"material_id"`
	Version    int64     `json:"version"`
	Reason     string    `json:"reason"`
}

type materialTimeTarget struct {
	cursor    MaterialTimeCursor
	revision  uuid.UUID
	tombstone bool
	at        time.Time
	problem   string
}

func BackfillMaterialTimes(ctx context.Context, conn *pgx.Conn, opts MaterialTimeOptions) (MaterialTimeReport, error) {
	report := MaterialTimeReport{NextAfter: &opts.After}
	if err := opts.Validate(); err != nil {
		return report, err
	}

	access := pgx.ReadOnly
	if opts.Apply {
		access = pgx.ReadWrite
	}
	for range opts.Batches {
		batch, err := scanMaterialTimeBatch(ctx, conn, access, opts)
		if err != nil {
			return report, err
		}
		report.Scanned += batch.Scanned
		report.Matched += batch.Matched
		report.Updated += batch.Updated
		report.Missing += batch.Missing
		report.Unsafe += batch.Unsafe
		report.Legacy += batch.Legacy
		room := materialTimeSample - len(report.Unresolved)
		report.Unresolved = append(report.Unresolved, batch.Unresolved[:min(room, len(batch.Unresolved))]...)
		report.UnresolvedTruncated = report.UnresolvedTruncated || len(batch.Unresolved) > room
		report.Complete, report.NextAfter = batch.Complete, batch.NextAfter
		if report.Complete {
			return report, nil
		}
		opts.After = *batch.NextAfter
	}

	return report, nil
}

func (opts MaterialTimeOptions) Validate() error {
	if opts.BatchSize < 1 || opts.BatchSize > MaxMaterialTimeBatch || opts.Batches < 1 || opts.Batches > MaxMaterialTimeBatches ||
		opts.EventLimit < 1 || opts.EventLimit > MaxMaterialTimeEvents || opts.After.Version < 0 || !validMaterialTimeTopic(opts.Topic) {
		return errors.New("invalid topic or batch limits")
	}

	return nil
}

func validMaterialTimeTopic(topic string) bool {
	if topic == "" || len(topic) > 249 || topic == "." || topic == ".." {
		return false
	}
	for _, c := range topic {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}

	return true
}

func scanMaterialTimeBatch(ctx context.Context, conn *pgx.Conn, access pgx.TxAccessMode, opts MaterialTimeOptions) (MaterialTimeReport, error) {
	ctx, cancel := context.WithTimeout(ctx, materialTimeTimeout)
	defer cancel()
	var report MaterialTimeReport
	err := pgx.BeginTxFunc(ctx, conn, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: access}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '8s'; SET LOCAL lock_timeout = '1s'"); err != nil {
			return err
		}
		targets, more, err := missingMaterialTimes(ctx, tx, opts)
		if err != nil {
			return err
		}
		report.Complete = !more
		if more {
			report.NextAfter = &targets[len(targets)-1].cursor
		}
		if err := findMaterialTimes(ctx, tx, targets, opts); err != nil {
			return err
		}

		return materialTimeBatchResult(ctx, tx, opts.Apply, targets, &report)
	})

	return report, err
}

func missingMaterialTimes(ctx context.Context, tx pgx.Tx, opts MaterialTimeOptions) ([]materialTimeTarget, bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT material_id, version, revision_id, tombstone
		FROM content_catalog_versions
		WHERE activity_occurred_at IS NULL AND (material_id, version) > ($1::uuid, $2::bigint)
		ORDER BY material_id, version LIMIT $3`, opts.After.MaterialID, opts.After.Version, opts.BatchSize+1)
	if err != nil {
		return nil, false, err
	}
	targets, err := pgx.AppendRows(make([]materialTimeTarget, 0, opts.BatchSize+1), rows, func(row pgx.CollectableRow) (materialTimeTarget, error) {
		var target materialTimeTarget
		scanErr := row.Scan(&target.cursor.MaterialID, &target.cursor.Version, &target.revision, &target.tombstone)

		return target, scanErr
	})
	if len(targets) > opts.BatchSize {
		return targets[:opts.BatchSize], true, err
	}

	return targets, false, err
}

func findMaterialTimes(ctx context.Context, tx pgx.Tx, targets []materialTimeTarget, opts MaterialTimeOptions) error {
	keys := make([]string, 0, len(targets))
	byKey := make(map[string]map[int64]*materialTimeTarget, len(targets))
	for i := range targets {
		target := &targets[i]
		if target.cursor.Version == 0 {
			continue
		}
		key := target.cursor.MaterialID.String()
		if byKey[key] == nil {
			byKey[key] = make(map[int64]*materialTimeTarget)
			keys = append(keys, key)
		}
		byKey[key][target.cursor.Version] = target
	}
	if len(keys) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
		SELECT k.material_id, e.id::text, CASE WHEN octet_length(e.message_value) <= $4 THEN e.message_value END
		FROM unnest($2::text[]) AS k(material_id)
		CROSS JOIN LATERAL (
		    SELECT id, message_value FROM outbox WHERE topic = $1 AND message_key = k.material_id
		    ORDER BY id LIMIT $3
		) AS e`, opts.Topic, keys, opts.EventLimit+1, materialEventSize)
	if err != nil {
		return err
	}
	defer rows.Close()
	counts := make(map[string]int, len(keys))
	uncertain := make(map[string]string, len(keys))
	for rows.Next() {
		var key, id string
		var raw []byte
		if err := rows.Scan(&key, &id, &raw); err != nil {
			return err
		}
		counts[key]++
		if counts[key] > opts.EventLimit {
			uncertain[key] = materialTimeLimit
		}
		if uncertain[key] != "" {

			continue
		}
		event, ok := decodeMaterialTimeEvent(id, key, raw)
		if !ok {
			uncertain[key] = materialTimeInvalid

			continue
		}
		if target := byKey[key][event.AggregateVersion]; target != nil {
			target.accept(event)
		}
	}
	for key, reason := range uncertain {
		for _, target := range byKey[key] {
			target.problem = reason
		}
	}

	return rows.Err()
}

type materialTimePayload struct {
	Tombstone             *bool                      `json:"tombstone"`
	Metadata              *activity.ResourceMetadata `json:"metadata"`
	InvalidatedRevisionID string                     `json:"invalidated_revision_id"`
}

func decodeMaterialTimeEvent(id, key string, raw []byte) (activity.Event[materialTimePayload], bool) {
	var event activity.Event[materialTimePayload]
	if len(raw) == 0 || len(raw) > materialEventSize || json.Unmarshal(raw, &event) != nil ||
		event.SchemaVersion != activity.SchemaVersion || event.EventID != id || event.AggregateID != key ||
		event.AggregateVersion < 1 || event.OccurredAt.IsZero() || event.Payload.Tombstone == nil || !canonicalMaterialUUID(event.EventID) ||
		!canonicalMaterialUUID(event.RevisionID) {
		return event, false
	}
	switch event.EventType {
	case activity.MaterialArchived:
		return event, *event.Payload.Tombstone && event.Payload.Metadata == nil
	case activity.MaterialWithdrawn:
		return event, *event.Payload.Tombstone && event.Payload.Metadata == nil && event.Payload.InvalidatedRevisionID == event.RevisionID
	case activity.MaterialCorrected:
		if !canonicalMaterialUUID(event.Payload.InvalidatedRevisionID) || event.Payload.InvalidatedRevisionID == event.RevisionID {
			return event, false
		}
	case activity.MaterialPublished, activity.MaterialRestored:
	default:
		return event, false
	}
	if *event.Payload.Tombstone || event.Payload.Metadata == nil {
		return event, false
	}
	title := strings.TrimSpace(event.Payload.Metadata.Title)

	return event, title != "" && !strings.ContainsRune(title, 0) && utf8.RuneCountInString(title) <= 256
}

func canonicalMaterialUUID(value string) bool {
	id, err := uuid.Parse(value)

	return err == nil && id != uuid.Nil && value == id.String()
}

func (target *materialTimeTarget) accept(event activity.Event[materialTimePayload]) {
	if !target.at.IsZero() || target.revision.String() != event.RevisionID || target.tombstone != *event.Payload.Tombstone {
		target.problem = materialTimeConflict

		return
	}
	target.at = event.OccurredAt.UTC().Truncate(time.Microsecond)
}

func materialTimeBatchResult(ctx context.Context, tx pgx.Tx, apply bool, targets []materialTimeTarget, report *MaterialTimeReport) error {
	ids := make([]uuid.UUID, 0, len(targets))
	versions := make([]int64, 0, len(targets))
	times := make([]time.Time, 0, len(targets))
	report.Scanned = int64(len(targets))
	for _, target := range targets {
		reason := target.problem
		switch {
		case target.cursor.Version == 0:
			report.Legacy++
			reason = "legacy"
		case reason != "":
			report.Unsafe++
		case target.at.IsZero():
			report.Missing++
			reason = "missing"
		default:
			report.Matched++
			if apply {
				ids = append(ids, target.cursor.MaterialID)
				versions = append(versions, target.cursor.Version)
				times = append(times, target.at)
			}
		}
		if reason != "" {
			report.Unresolved = append(report.Unresolved, MaterialTimeProblem{
				MaterialID: target.cursor.MaterialID, Version: target.cursor.Version, Reason: reason,
			})
		}
	}
	if len(ids) == 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, `
		UPDATE content_catalog_versions AS v SET activity_occurred_at = planned.at
		FROM unnest($1::uuid[], $2::bigint[], $3::timestamptz[]) AS planned(id, version, at)
		WHERE (v.material_id, v.version) = (planned.id, planned.version) AND v.activity_occurred_at IS NULL`, ids, versions, times)
	if err != nil {
		return err
	}
	report.Updated = tag.RowsAffected()

	return nil
}
