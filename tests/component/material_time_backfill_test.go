//go:build component

package component

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/pkg/api/activity"
)

func TestMaterialTimeBackfill(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	conn, connectErr := pgx.ConnectConfig(ctx, testDatabaseConfig(t).ConnConfig)
	if connectErr != nil {
		t.Fatal(connectErr)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TEMP TABLE content_catalog_versions (LIKE public.content_catalog_versions INCLUDING ALL)`)
	exec(`CREATE TEMP TABLE outbox (LIKE public.outbox INCLUDING ALL)`)

	const topic = "material.archive.v1"
	revision := uuid.NewString()
	at := time.Date(2026, time.August, 3, 14, 30, 0, 321987654, time.UTC)
	key := func(i int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", i) }
	addVersion := func(i int, version int64, hidden bool, known *time.Time) {
		t.Helper()
		var metadata *string
		if !hidden {
			title := `{"title":"Graph algorithms"}`
			metadata = &title
		}
		exec(`INSERT INTO pg_temp.content_catalog_versions (material_id, version, revision_id, tombstone, metadata, activity_occurred_at)
		      VALUES ($1, $2, $3, $4, $5::jsonb, $6)`, key(i), version, revision, hidden, metadata, known)
	}
	addEvent := func(stream string, i int, version int64, rev, kind string, stamp time.Time, state activity.MaterialState) {
		t.Helper()
		id := uuid.NewString()
		raw, err := json.Marshal(activity.Event[activity.MaterialState]{
			EventID: id, EventType: kind, OccurredAt: stamp, SchemaVersion: 1, AggregateID: key(i),
			AggregateVersion: version, RevisionID: rev, Payload: state,
		})
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO pg_temp.outbox (id, topic, message_key, message_value) VALUES ($1, $2, $3, $4)`, id, stream, key(i), raw)
	}
	visible := activity.MaterialState{Metadata: &activity.ResourceMetadata{Title: "Graph algorithms", Type: "note", Format: "markdown"}}
	for _, row := range []struct {
		i       int
		version int64
		hidden  bool
	}{
		{1, 0, true}, {1, 1, false}, {1, 2, true}, {2, 3, false}, {2, 5, false},
		{3, 4, false}, {4, 6, false}, {5, 7, false}, {7, 9, false},
	} {
		addVersion(row.i, row.version, row.hidden, nil)
	}
	known := at.Add(time.Hour).Truncate(time.Microsecond)
	addVersion(6, 8, false, &known)
	addEvent(topic, 1, 2, revision, activity.MaterialWithdrawn, at.Add(time.Minute),
		activity.MaterialState{Tombstone: true, InvalidatedRevisionID: revision})
	addEvent(topic, 1, 1, revision, activity.MaterialPublished, at, visible)
	addEvent(topic, 2, 3, revision, activity.MaterialRestored, at.Add(-time.Minute), visible)
	addEvent("another.topic", 2, 5, revision, activity.MaterialPublished, at, visible)
	addEvent(topic, 3, 4, revision, activity.MaterialPublished, at, visible)
	addEvent(topic, 3, 4, revision, activity.MaterialPublished, at.Add(time.Second), visible)
	exec(`INSERT INTO pg_temp.outbox (topic, message_key, message_value) VALUES ($1, $2, $3)`, topic, key(4), []byte("{"))
	for _, version := range []int64{7, 70, 71} {
		addEvent(topic, 5, version, revision, activity.MaterialPublished, at, visible)
	}
	addEvent(topic, 6, 8, revision, activity.MaterialPublished, at, visible)
	addEvent(topic, 7, 9, uuid.NewString(), activity.MaterialPublished, at, visible)

	opts := postgres.MaterialTimeOptions{Topic: topic, BatchSize: 2, Batches: 2, EventLimit: 2}
	report, err := postgres.BackfillMaterialTimes(ctx, conn, opts)
	if err != nil || report.Complete || report.NextAfter == nil || report.NextAfter.MaterialID.String() != key(2) ||
		report.NextAfter.Version != 3 || report.Scanned != 4 || report.Matched != 3 || report.Legacy != 1 || report.Updated != 0 {
		t.Fatal("wrong read-only report", report, err)
	}
	resume := opts
	resume.After, resume.Batches = *report.NextAfter, 3
	report, err = postgres.BackfillMaterialTimes(ctx, conn, resume)
	if err != nil || !report.Complete || report.NextAfter != nil || report.Missing != 1 || report.Unsafe != 4 || report.Scanned != 5 {
		t.Fatal("wrong resumed report", report, err)
	}
	reasons := make([]string, 0, len(report.Unresolved))
	for _, issue := range report.Unresolved {
		reasons = append(reasons, issue.Reason)
	}
	if !slices.Equal(reasons, []string{"missing", "conflict", "invalid_event", "event_limit", "conflict"}) || report.UnresolvedTruncated {
		t.Fatal("wrong unresolved reasons", reasons)
	}
	checkWritten := func(want []int64) {
		t.Helper()
		rows, queryErr := conn.Query(ctx, `SELECT version FROM pg_temp.content_catalog_versions
		                              WHERE activity_occurred_at IS NOT NULL ORDER BY activity_occurred_at`)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		got, collectErr := pgx.CollectRows(rows, pgx.RowTo[int64])
		if collectErr != nil || !slices.Equal(got, want) {
			t.Fatal("wrong version order", got, collectErr)
		}
	}
	checkWritten([]int64{8})

	opts.Apply, opts.BatchSize, opts.Batches = true, 10, 1
	exec(`ALTER TABLE pg_temp.content_catalog_versions ADD CONSTRAINT backfill_fixture_rejected
	      CHECK (version <> 2 OR activity_occurred_at IS NULL)`)
	if failed, batchErr := postgres.BackfillMaterialTimes(ctx, conn, opts); batchErr == nil || failed.Updated != 0 {
		t.Fatal("failed batch must not report writes", failed, batchErr)
	}
	checkWritten([]int64{8})
	exec(`ALTER TABLE pg_temp.content_catalog_versions DROP CONSTRAINT backfill_fixture_rejected`)
	report, err = postgres.BackfillMaterialTimes(ctx, conn, opts)
	if err != nil || !report.Complete || report.Updated != 3 || report.Missing != 1 || report.Unsafe != 4 {
		t.Fatal("wrong apply report", report, err)
	}
	checkWritten([]int64{3, 1, 2, 8})
	var stamp time.Time
	if scanErr := conn.QueryRow(ctx, `SELECT activity_occurred_at FROM pg_temp.content_catalog_versions WHERE version = 1`).Scan(&stamp); scanErr != nil ||
		!stamp.Equal(at.Truncate(time.Microsecond)) {
		t.Fatal("timestamp changed when normalized to PostgreSQL precision", stamp, scanErr)
	}
	report, err = postgres.BackfillMaterialTimes(ctx, conn, opts)
	if err != nil || !report.Complete || report.Updated != 0 || report.Scanned != 6 {
		t.Fatal("rerun rewrote a timestamp", report, err)
	}
	checkWritten([]int64{3, 1, 2, 8})
	exec(`INSERT INTO pg_temp.content_catalog_versions (material_id, version, revision_id, tombstone)
	      SELECT gen_random_uuid(), 20 + n, gen_random_uuid(), true FROM generate_series(1, 51) n`)
	opts.Apply, opts.BatchSize, opts.Batches = false, 25, 3
	report, err = postgres.BackfillMaterialTimes(ctx, conn, opts)
	if err != nil || !report.Complete || !report.UnresolvedTruncated || len(report.Unresolved) != 50 || report.Missing != 52 {
		t.Fatal("large report must keep accurate counts and a bounded sample", report, err)
	}
}
