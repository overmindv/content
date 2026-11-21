//go:build component

package component

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/pkg/api/activity"
	"github.com/overmindv/content/tests/builders"
	"github.com/overmindv/parker"
)

func TestMaterialModeration(t *testing.T) {
	f := newActivityFixture(t)
	file := uuid.NewString()
	draft := builders.ContentItem()
	draft.CreatedBy, draft.Slug = f.owner, draft.Slug+"-"+uuid.NewString()
	draft.Assets = []domain.CreateContentAssetInput{{AssetID: file, Kind: domain.AssetKindAttachment}}
	created, err := f.store.Create(f.ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	item := f.state(t, created, domain.ContentStatusPublished)
	attachment := domain.PublishedAttachmentRef{MaterialID: item.ID, RevisionID: item.PublishedRevisionID, FileID: file}
	if err := f.store.CheckPublishedAttachment(f.ctx, attachment); err != nil {
		t.Fatal(err)
	}
	actor, reader := uuid.NewString(), uuid.NewString()
	proposal, err := f.store.OpenChangeProposal(f.ctx, domain.OpenChangeProposalInput{
		ContentItemID: item.ID, BaseRevisionID: item.PublishedRevisionID, AuthorID: reader, Source: "# Suggestion",
	})
	if err != nil {
		t.Fatal(err)
	}
	input := domain.SetMaterialModerationInput{
		ActorID: actor, Reason: "private moderator evidence",
		MaterialModeration: domain.MaterialModeration{
			MaterialID: item.ID, PublishedRevisionID: item.PublishedRevisionID, Hidden: true,
		},
	}
	initial, err := f.store.GetMaterialModeration(f.ctx, item.ID)
	if err != nil || initial.Hidden || initial.Version != 0 || initial.PublishedRevisionID != item.PublishedRevisionID {
		t.Fatal("initial moderation state", initial, err)
	}
	wrongRevision := input
	wrongRevision.PublishedRevisionID = uuid.NewString()
	if _, err := f.store.SetMaterialModeration(f.ctx, wrongRevision); !errors.Is(err, service.ErrModerationConflict) {
		t.Errorf("stale publication guard: %v", err)
	}
	hidden, err := f.store.SetMaterialModeration(f.ctx, input)
	if err != nil || hidden.Version != 1 || !hidden.Hidden {
		t.Fatal("hide", hidden, err)
	}
	if _, err := f.store.SetMaterialModeration(f.ctx, input); !errors.Is(err, service.ErrModerationConflict) {
		t.Errorf("replayed hide: %v", err)
	}

	if _, err := f.store.Get(f.ctx, item.ID, reader); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("hidden item could be fetched: %v", err)
	}
	if err := f.store.CheckPublishedAttachment(f.ctx, attachment); !errors.Is(err, service.ErrNotFound) {
		t.Error("hidden attachment available", err)
	}
	if _, err := f.store.Get(f.ctx, item.ID, f.owner); err != nil {
		t.Errorf("owner lost access to history: %v", err)
	}
	items, err := f.store.List(f.ctx, reader)
	if err != nil || slices.ContainsFunc(items, func(v domain.ContentItem) bool { return v.ID == item.ID }) {
		t.Errorf("hidden item listed for reader: %v", err)
	}
	history, err := f.store.ListRevisions(f.ctx, item.ID, f.owner)
	if err != nil || len(history) != 1 || history[0].ID != item.PublishedRevisionID {
		t.Errorf("moderation changed revision history: %v", err)
	}
	catalog, err := f.store.ListCatalog(f.ctx, domain.CatalogFilter{ActorID: f.owner, AuthorID: f.owner, PageSize: 10}, nil)
	if err != nil || len(catalog) != 0 {
		t.Errorf("hidden item listed in catalog: %v", err)
	}
	if err := f.store.ConfirmMaterialRead(f.ctx, domain.ConfirmMaterialReadInput{
		MaterialID: item.ID, RevisionID: item.PublishedRevisionID, ActorID: reader,
	}); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("hidden material accepted read confirmation: %v", err)
	}
	if _, err := f.store.GetChangeProposalDiffSources(f.ctx, domain.ChangeProposalDiffInput{
		ProposalID: proposal.ID, ActorID: reader,
	}); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("hidden material could be read through diff: %v", err)
	}
	if _, err := f.store.OpenChangeProposal(f.ctx, domain.OpenChangeProposalInput{
		ContentItemID: item.ID, BaseRevisionID: item.PublishedRevisionID, AuthorID: uuid.NewString(), Source: "# Another edit",
	}); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("opened a proposal for hidden material: %v", err)
	}
	if _, err := f.store.MergeChangeProposal(f.ctx, domain.MergeChangeProposalInput{
		ProposalID: proposal.ID, ExpectedHeadID: proposal.Head.ID, ActorID: f.owner,
	}); !errors.Is(err, service.ErrInvalidState) {
		t.Errorf("owner could bypass moderation through merge: %v", err)
	}
	if _, err := f.store.RestorePublishedRevision(f.ctx, domain.RestorePublishedRevisionInput{
		ContentItemID: item.ID, ExpectedPublishedID: item.PublishedRevisionID, SourceRevisionID: uuid.NewString(), ActorID: f.owner,
	}); !errors.Is(err, service.ErrInvalidState) {
		t.Errorf("owner could bypass moderation through rollback: %v", err)
	}
	if _, err := f.store.ChangeState(f.ctx, domain.ChangeContentItemStateInput{
		ID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, ActorID: f.owner,
	}, domain.ContentStatusPublished); !errors.Is(err, service.ErrInvalidState) {
		t.Errorf("owner could bypass moderation through publish: %v", err)
	}

	before := readActivity[activity.MaterialState](t, f, f.material)
	if len(before) != 2 || before[1].EventType != activity.MaterialWithdrawn ||
		!before[1].Payload.Tombstone || before[1].Payload.Metadata != nil ||
		before[1].Payload.InvalidatedRevisionID != item.PublishedRevisionID {
		t.Fatal("hide did not produce a single withdrawal without metadata")
	}
	hiddenWatermark := before[1].AggregateVersion
	if snapshot := findModeratedSnapshot(t, f, item.ID, &hiddenWatermark); !snapshot.Tombstone || snapshot.Metadata != nil {
		t.Error("hidden material remains visible in the snapshot")
	}
	restore := input
	restore.Hidden, restore.Version, restore.Reason = false, hidden.Version, "reviewed"
	visible, err := f.store.SetMaterialModeration(f.ctx, restore)
	if err != nil || visible.Version != 2 || visible.Hidden || visible.PublishedRevisionID != item.PublishedRevisionID {
		t.Fatal("restore", visible, err)
	}
	if _, err := f.store.SetMaterialModeration(f.ctx, input); !errors.Is(err, service.ErrModerationConflict) {
		t.Errorf("old hide could overwrite restore: %v", err)
	}
	if _, err := f.store.Get(f.ctx, item.ID, reader); err != nil {
		t.Errorf("reader cannot see restored material: %v", err)
	}
	if err := f.store.CheckPublishedAttachment(f.ctx, attachment); err != nil {
		t.Error("restored attachment unavailable", err)
	}
	if snapshot := findModeratedSnapshot(t, f, item.ID, nil); snapshot.Tombstone || snapshot.Metadata == nil {
		t.Error("restored material missing from snapshot")
	}
	if snapshot := findModeratedSnapshot(t, f, item.ID, &hiddenWatermark); !snapshot.Tombstone {
		t.Error("restore changed a pinned catalog snapshot")
	}

	events := readActivity[activity.MaterialState](t, f, f.material)
	if len(events) != 3 || events[2].EventType != activity.MaterialRestored || events[2].Payload.Metadata == nil ||
		events[0].AggregateVersion >= events[1].AggregateVersion || events[1].AggregateVersion >= events[2].AggregateVersion {
		t.Fatal("moderation events do not form an ordered state history")
	}
	var latest activity.Event[activity.MaterialState]
	apply := func(event activity.Event[activity.MaterialState]) {
		if event.AggregateVersion > latest.AggregateVersion {
			latest = event
		}
	}
	apply(events[1])
	apply(events[0])
	if !latest.Payload.Tombstone {
		t.Error("old publish resurrected the hidden material")
	}
	apply(events[2])
	apply(events[1])
	apply(events[2])
	if latest.Payload.Tombstone || latest.EventID != events[2].EventID {
		t.Error("reordered or repeated delivery reverted the restoration")
	}

	if _, err := f.store.Delete(f.ctx, item.ID, f.owner); err != nil {
		t.Fatal(err)
	}
	eventReason, exists := "", false
	var count int
	err = f.pool.QueryRow(f.ctx, `
		SELECT count(*), COALESCE(max(reason) FILTER (WHERE version = 1), ''),
		       bool_and(actor_id = $2 AND published_revision_id = $3 AND hidden = (version = 1))
		FROM content_moderation_audit WHERE material_id = $1
	`, item.ID, actor, item.PublishedRevisionID).Scan(&count, &eventReason, &exists)
	if err != nil || count != 2 || eventReason != input.Reason || !exists {
		t.Errorf("audit must retain one entry per moderation after deletion: count=%d err=%v", count, err)
	}
}

func findModeratedSnapshot(t *testing.T, f activityFixture, id string, watermark *int64) activity.MaterialState {
	t.Helper()
	input := domain.MaterialSnapshotInput{PageSize: 20, Watermark: watermark}
	for {
		page, err := f.store.SnapshotMaterials(f.ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Entries {
			if entry.MaterialID == id {
				return entry.State
			}
		}
		if page.NextAfterID == "" {
			t.Fatal("material not in snapshot")
		}
		input.AfterID, input.Watermark = page.NextAfterID, &page.Watermark
	}
}

func TestModerationKafkaDelivery(t *testing.T) {
	broker, topic := os.Getenv("COMPONENT_TEST_KAFKA_BROKER"), os.Getenv("COMPONENT_TEST_MATERIAL_TOPIC")
	if broker == "" || topic == "" {
		t.Skip("requires dedicated Kafka topic and an isolated database")
	}
	f := newActivityFixture(t)
	f.material = topic
	f.store = postgres.NewContentItemStore(f.pool, postgres.WithMaterialActivity(topic, ""))
	item := f.state(t, f.create(t), domain.ContentStatusPublished)
	published := publishWithoutBaseAttachments(t, f, item)
	input := domain.SetMaterialModerationInput{
		ActorID: uuid.NewString(), Reason: "not sent to Kafka",
		MaterialModeration: domain.MaterialModeration{
			MaterialID: item.ID, PublishedRevisionID: published, Hidden: true,
		},
	}
	hidden, err := f.store.SetMaterialModeration(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Hidden, input.Version = false, hidden.Version
	if _, err = f.store.SetMaterialModeration(f.ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.RestorePublishedRevision(f.ctx, domain.RestorePublishedRevisionInput{
		ContentItemID: item.ID, ExpectedPublishedID: published, SourceRevisionID: item.PublishedRevisionID, ActorID: f.owner,
	}); err != nil {
		t.Fatal(err)
	}
	expected := readActivity[activity.MaterialState](t, f, topic)
	if len(expected) != 5 {
		t.Fatal("expected five events on the dedicated topic")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	brokers := []string{broker}
	producer, err := parker.NewProducer(brokers)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if err = producer.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	events := make(chan activity.Event[activity.MaterialState], 8)
	subscriber, err := parker.NewSubscriber(brokers, "moderation-test-"+uuid.NewString(), []string{topic},
		func(ctx context.Context, key string, value []byte) error {
			if key != item.ID {
				return nil
			}
			var event activity.Event[activity.MaterialState]
			if err := json.Unmarshal(value, &event); err != nil {
				return err
			}
			select {
			case events <- event:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer subscriber.Close()
	dispatcher := parker.NewOutboxDispatcher(parker.NewPgOutbox(f.pool), producer, 25*time.Millisecond, logger)
	dispatched, consumed := make(chan error, 1), make(chan error, 1)
	go func() { dispatched <- dispatcher.Run(ctx) }()
	go func() { consumed <- subscriber.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-dispatched; err != nil {
			t.Error(err)
		}
		if err := <-consumed; err != nil {
			t.Error(err)
		}
	}()
	seen := make(map[string]bool, len(expected))
	for len(seen) < len(expected) {
		select {
		case event := <-events:
			pos := slices.IndexFunc(expected, func(want activity.Event[activity.MaterialState]) bool {
				return event.EventID == want.EventID && event.RevisionID == want.RevisionID &&
					event.AggregateID == item.ID && event.EventType == want.EventType &&
					event.AggregateVersion == want.AggregateVersion && event.Payload.Tombstone == want.Payload.Tombstone &&
					event.Payload.InvalidatedRevisionID == want.Payload.InvalidatedRevisionID
			})
			if pos < 0 {
				t.Fatal("Kafka event does not match the persisted outbox")
			}
			seen[event.EventID] = true
		case <-ctx.Done():
			t.Fatal("moderation events received", len(seen), ctx.Err())
		}
	}
}
