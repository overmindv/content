//go:build component

package component

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/pkg/api/activity"
	"github.com/overmindv/content/tests/builders"
)

type activityFixture struct {
	ctx                           context.Context
	pool                          *pgxpool.Pool
	store                         *postgres.ContentItemStore
	material, editorial, learning string
	owner, topic                  string
}

func newActivityFixture(t *testing.T) activityFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	suffix := "-" + uuid.NewString()
	f := activityFixture{
		ctx: ctx, pool: pool, owner: uuid.NewString(), topic: uuid.NewString(),
		material: "material" + suffix, editorial: "editorial" + suffix, learning: "learning" + suffix,
	}
	f.store = postgres.NewContentItemStore(pool, postgres.WithActivityOutbox(f.material, f.editorial, f.learning))
	return f
}

func (f activityFixture) create(t *testing.T) domain.ContentItem {
	t.Helper()
	input := builders.ContentItem()
	input.CreatedBy, input.Slug = f.owner, input.Slug+"-"+uuid.NewString()
	input.Source, input.Description = "# Source should never be in activity", "Private description marker"
	input.Learning = &domain.LearningMetadata{TopicIDs: []string{f.topic}, EstimatedMinutes: 17}
	item, err := f.store.Create(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f activityFixture) state(t *testing.T, item domain.ContentItem, status domain.ContentStatus) domain.ContentItem {
	t.Helper()
	changed, err := f.store.ChangeState(f.ctx, domain.ChangeContentItemStateInput{
		ID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, ActorID: f.owner,
	}, status)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func readActivity[P any](t *testing.T, f activityFixture, topic string) []activity.Event[P] {
	t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT id, message_key, message_value FROM outbox WHERE topic = $1 ORDER BY created_at, id`, topic)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var events []activity.Event[P]
	for rows.Next() {
		var id, key string
		var payload []byte
		var event activity.Event[P]
		if err := rows.Scan(&id, &key, &payload); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(payload, &event); err != nil || event.EventID != id || event.AggregateID != key || event.SchemaVersion != 1 {
			t.Fatalf("outbox envelope mismatch: %v", err)
		}
		for _, forbidden := range []string{"Source should never", "Private description marker", "Unpublished draft marker", `"source"`, `"reason"`} {
			if strings.Contains(string(payload), forbidden) {
				t.Errorf("activity contains %q", forbidden)
			}
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func TestMaterialSnapshotAndReplay(t *testing.T) {
	f := newActivityFixture(t)
	first := f.state(t, f.create(t), domain.ContentStatusPublished)
	second := f.state(t, f.create(t), domain.ContentStatusPublished)
	draft := f.create(t)
	edited, err := f.store.CreateRevision(f.ctx, domain.CreateContentRevisionInput{
		ContentItemID: first.ID, ExpectedRevisionID: first.CurrentRevisionID, CreatedBy: f.owner,
		Format: domain.ContentFormatMarkdown, Source: "# Unpublished draft marker",
	})
	if err != nil {
		t.Fatal(err)
	}
	first.CurrentRevisionID = edited.ID
	reader := uuid.NewString()
	if _, err = f.store.Get(f.ctx, first.ID, reader); err != nil {
		t.Fatal(err)
	}
	if got := readActivity[activity.ReadConfirmation](t, f, f.learning); len(got) != 0 {
		t.Error("view emitted learning evidence")
	}
	read := domain.ConfirmMaterialReadInput{MaterialID: first.ID, RevisionID: edited.ID, ActorID: reader}
	if err = f.store.ConfirmMaterialRead(f.ctx, read); !errors.Is(err, service.ErrRevisionConflict) {
		t.Errorf("confirmed an unpublished revision: %v", err)
	}
	read.RevisionID = first.PublishedRevisionID
	for range 2 {
		if err = f.store.ConfirmMaterialRead(f.ctx, read); err != nil {
			t.Fatal(err)
		}
	}
	confirmed := readActivity[activity.ReadConfirmation](t, f, f.learning)
	if len(confirmed) != 1 || confirmed[0].EventType != activity.MaterialReadConfirmed ||
		confirmed[0].Payload.Evidence != activity.SelfReported || confirmed[0].Payload.UserID != reader ||
		confirmed[0].RevisionID != first.PublishedRevisionID {
		t.Errorf("invalid read confirmations: %+v", confirmed)
	}

	page, err := f.store.SnapshotMaterials(f.ctx, domain.MaterialSnapshotInput{PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	watermark := page.Watermark
	snapshot := make(map[string]domain.MaterialVersion)
	collect := func(page domain.MaterialSnapshot, to map[string]domain.MaterialVersion) {
		t.Helper()
		for {
			for _, e := range page.Entries {
				to[e.MaterialID] = e
			}
			if page.NextAfterID == "" {
				return
			}
			next, err := f.store.SnapshotMaterials(f.ctx, domain.MaterialSnapshotInput{
				Watermark: &page.Watermark, AfterID: page.NextAfterID, PageSize: 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			page = next
		}
	}
	f.state(t, first, domain.ContentStatusArchived)
	f.state(t, first, domain.ContentStatusArchived)
	if _, err = f.store.Delete(f.ctx, second.ID, f.owner); err != nil {
		t.Fatal(err)
	}
	third := f.state(t, f.create(t), domain.ContentStatusPublished)
	collect(page, snapshot)
	for _, item := range []domain.ContentItem{first, second} {
		version, found := snapshot[item.ID]
		if !found || version.State.Tombstone || version.RevisionID != item.PublishedRevisionID || version.State.Metadata == nil ||
			version.State.Metadata.EstimatedMinutes != 17 || !slices.Equal(version.State.Metadata.TopicIDs, []string{f.topic}) {
			t.Errorf("publication changed in old snapshot: %+v", version)
		}
	}
	for _, id := range []string{draft.ID, third.ID} {
		if _, found := snapshot[id]; found {
			t.Error("old snapshot contains a draft or later publication")
		}
	}
	events := readActivity[activity.MaterialState](t, f, f.material)
	if len(events) != 5 {
		t.Fatalf("outbox event count: %d", len(events))
	}
	slices.Reverse(events)
	for range 2 {
		for _, e := range events {
			if e.AggregateVersion > watermark && e.AggregateVersion > snapshot[e.AggregateID].Version {
				snapshot[e.AggregateID] = domain.MaterialVersion{
					MaterialID: e.AggregateID, RevisionID: e.RevisionID, Version: e.AggregateVersion, State: e.Payload,
				}
			}
		}
	}
	current, err := f.store.SnapshotMaterials(f.ctx, domain.MaterialSnapshotInput{PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	fresh := make(map[string]domain.MaterialVersion)
	collect(current, fresh)
	for _, item := range []domain.ContentItem{first, second, third} {
		replayed, latest := snapshot[item.ID], fresh[item.ID]
		tombstone := item.ID != third.ID
		if latest.Version != replayed.Version || latest.State.Tombstone != tombstone ||
			replayed.State.Tombstone != tombstone || (latest.State.Metadata == nil) != tombstone {
			t.Errorf("snapshot and replay disagree: %+v %+v", replayed, latest)
		}
	}
	if current.Watermark != watermark+3 {
		t.Errorf("expected three new catalog changes, got %d", current.Watermark-watermark)
	}
	broken := postgres.NewContentItemStore(f.pool, postgres.WithActivityOutbox("invalid\x00", f.editorial, f.learning))
	if _, err = broken.ChangeState(f.ctx, domain.ChangeContentItemStateInput{
		ID: draft.ID, ExpectedRevisionID: draft.CurrentRevisionID, ActorID: f.owner,
	}, domain.ContentStatusPublished); err == nil || !strings.Contains(err.Error(), "enqueue material activity") {
		t.Fatalf("expected outbox error: %v", err)
	}
	view, err := f.store.Get(f.ctx, draft.ID, f.owner)
	if err != nil || view.Status != domain.ContentStatusDraft || view.PublishedRevisionID != "" {
		t.Errorf("publication did not roll back: %+v, %v", view, err)
	}
	after, err := f.store.SnapshotMaterials(f.ctx, domain.MaterialSnapshotInput{PageSize: 1})
	if err != nil || after.Watermark != current.Watermark {
		t.Errorf("watermark advanced after rollback: %d, %v", after.Watermark, err)
	}
}

func TestEditorialActivityAfterMerge(t *testing.T) {
	f := newActivityFixture(t)
	item := f.state(t, f.create(t), domain.ContentStatusPublished)
	author, dissent, third := uuid.NewString(), uuid.NewString(), uuid.NewString()
	p, err := f.store.OpenChangeProposal(f.ctx, domain.OpenChangeProposalInput{
		ContentItemID: item.ID, BaseRevisionID: item.PublishedRevisionID, AuthorID: author, Source: "# Accepted edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	assign := func(ids ...string) {
		t.Helper()
		if err := f.store.AssignReviewers(f.ctx, domain.ReviewAssignmentRequest{
			ProposalID: p.ID, RevisionID: p.Head.ID, TopicID: f.topic, RankedCandidateIDs: ids,
		}); err != nil {
			t.Fatal(err)
		}
	}
	vote := func(reviewer string, decision domain.ReviewDecisionKind) {
		t.Helper()
		if _, err := f.store.SubmitReviewDecision(f.ctx, domain.SubmitReviewDecisionInput{
			ReviewRevisionRef: domain.ReviewRevisionRef{
				ReviewActor: domain.ReviewActor{ProposalID: p.ID, ActorID: reviewer}, RevisionID: p.Head.ID,
			}, Decision: decision, Reason: "Review reason stays private",
		}); err != nil {
			t.Fatal(err)
		}
	}
	assign(f.owner, dissent)
	vote(f.owner, domain.ReviewApprove)
	vote(dissent, domain.ReviewRequestChanges)
	assign(third)
	vote(third, domain.ReviewApprove)
	if got := readActivity[activity.EditorialContribution](t, f, f.editorial); len(got) != 0 {
		t.Error("review emitted rating activity before merge")
	}
	merge := domain.MergeChangeProposalInput{ProposalID: p.ID, ExpectedHeadID: p.Head.ID, ActorID: f.owner}
	merged, err := f.store.MergeChangeProposal(f.ctx, merge)
	if err != nil {
		t.Fatal(err)
	}
	if replayed, err := f.store.MergeChangeProposal(f.ctx, merge); err != nil || replayed != merged {
		t.Fatalf("merge retry: %+v %v", replayed, err)
	}
	events := readActivity[activity.EditorialContribution](t, f, f.editorial)
	if len(events) != 4 {
		t.Fatalf("editorial event count: %d", len(events))
	}
	reviews := make(map[string]string)
	var contributions int
	for _, e := range events {
		if e.RevisionID != merged.PublishedRevisionID || e.Payload.AuthorID != author || e.Payload.TopicID != f.topic ||
			e.Payload.ProposalID != p.ID || e.Payload.ProposalRevisionID != p.Head.ID {
			t.Errorf("event refers to the wrong review: %+v", e)
		}
		if e.EventType == activity.ContributionMerged {
			contributions++
		}
		if e.EventType == activity.ReviewAccepted {
			reviews[e.Payload.ReviewerID] = e.Payload.Decision
		}
	}
	if contributions != 1 || len(reviews) != 3 || reviews[f.owner] != "approve" || reviews[dissent] != "request_changes" || reviews[third] != "approve" {
		t.Errorf("contributions or decisions lost: %d %+v", contributions, reviews)
	}
	if got := readActivity[activity.MaterialState](t, f, f.material); len(got) != 2 || got[1].RevisionID != merged.PublishedRevisionID {
		t.Errorf("merged version missing from material stream: %+v", got)
	}
}
