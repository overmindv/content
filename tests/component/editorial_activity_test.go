//go:build component

package component

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/pkg/api/activity"
)

func TestEditorialActivityUsesOnlyAcceptedRevision(t *testing.T) {
	topic := "editorial-" + uuid.NewString()
	f := newMergeFixture(t, postgres.WithEditorialActivity(topic))
	start := f.proposal(uuid.NewString(), "# Old review revision", nil)
	previousReviewer := f.approve(start)
	p, err := f.store.AppendChangeProposalRevision(t.Context(), domain.AppendChangeProposalRevisionInput{
		ProposalID: start.ID, ExpectedHeadID: start.Head.ID, AuthorID: start.AuthorID, Source: "# Revised text",
	})
	if err != nil {
		t.Fatal(err)
	}
	revoked, changes, tiebreaker := uuid.NewString(), uuid.NewString(), uuid.NewString()
	assign := func(candidates ...string) {
		t.Helper()
		if err := f.store.AssignReviewers(t.Context(), domain.ReviewAssignmentRequest{
			ProposalID: p.ID, RevisionID: p.Head.ID, TopicID: f.topic, RankedCandidateIDs: candidates,
		}); err != nil {
			t.Fatal(err)
		}
	}
	vote := func(id string, decision domain.ReviewDecisionKind) {
		t.Helper()
		if _, err := f.store.SubmitReviewDecision(t.Context(), domain.SubmitReviewDecisionInput{
			ReviewRevisionRef: domain.ReviewRevisionRef{
				ReviewActor: domain.ReviewActor{ProposalID: p.ID, ActorID: id}, RevisionID: p.Head.ID,
			},
			Decision: decision, Reason: "private-review-reason",
		}); err != nil {
			t.Fatal(err)
		}
	}
	assign(f.owner, revoked)
	vote(f.owner, domain.ReviewApprove)
	vote(revoked, domain.ReviewApprove)
	if err = f.store.RevokeReviewAssignment(t.Context(), p.ID, p.Head.ID, revoked); err != nil {
		t.Fatal(err)
	}
	assign(changes)
	vote(changes, domain.ReviewRequestChanges)
	assign(tiebreaker)
	vote(tiebreaker, domain.ReviewApprove)
	if count := countEditorial(t, f, topic); count != 0 {
		t.Fatal("review before merge emitted events", count)
	}

	merged, err := f.merge(p)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := f.merge(p); err != nil || replay != merged {
		t.Fatal("merge replay changed publication", err)
	}

	var version int64
	if err = f.pool.QueryRow(t.Context(), `
		SELECT version FROM content_catalog_versions WHERE material_id = $1 AND revision_id = $2`,
		f.item.ID, merged.PublishedRevisionID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	rows, err := f.pool.Query(t.Context(), `SELECT id, message_value FROM outbox WHERE message_key = $1 AND topic = $2`, f.item.ID, topic)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string]string{f.owner: "approve", changes: "request_changes", tiebreaker: "approve"}
	authors := 0
	for rows.Next() {
		var id string
		var body []byte
		var event activity.Event[activity.EditorialContribution]
		if err = rows.Scan(&id, &body); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		if event.EventID != id || event.AggregateID != f.item.ID || event.AggregateVersion != version ||
			event.SchemaVersion != activity.SchemaVersion || event.RevisionID != merged.PublishedRevisionID ||
			event.Payload.ProposalID != p.ID || event.Payload.ProposalRevisionID != p.Head.ID ||
			event.Payload.AuthorID != p.AuthorID || event.Payload.TopicID != f.topic {
			t.Error("editorial event did not identify the published revision")
		}
		for _, forbidden := range []string{"# Revised text", "private-review-reason", start.Head.ID, previousReviewer, revoked} {
			if bytes.Contains(body, []byte(forbidden)) {
				t.Error("editorial event exposed private source or an inactive reviewer")
			}
		}
		switch event.EventType {
		case activity.ContributionMerged:
			authors++
			if event.Payload.ReviewerID != "" || event.Payload.Decision != "" {
				t.Error("author event includes a reviewer decision")
			}
		case activity.ReviewAccepted:
			if decision, ok := want[event.Payload.ReviewerID]; !ok || decision != event.Payload.Decision {
				t.Error("unexpected or duplicated reviewer in editorial activity")
			}
			delete(want, event.Payload.ReviewerID)
		default:
			t.Error("unexpected event type", event.EventType)
		}
	}
	if err = rows.Err(); err != nil || authors != 1 || len(want) != 0 {
		t.Fatal("expected one author event and one per counted review", err, authors, len(want))
	}
	var leaked int
	if err = f.pool.QueryRow(t.Context(), `
		SELECT count(*) FROM outbox WHERE message_key = $1 AND topic = $2
		  AND convert_from(message_value, 'UTF8') LIKE ANY(ARRAY['%author_id%', '%reviewer_id%', '%contribution.merged%'])
	`, f.item.ID, mergeMaterialTopic).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatal("editorial data reached the public catalog", err, leaked)
	}
}

func TestEditorialOutboxFailureRollsBackMerge(t *testing.T) {
	topic := "editorial-" + uuid.NewString()
	f := newMergeFixture(t, postgres.WithEditorialActivity(topic))
	p := f.proposal(uuid.NewString(), "# Test outbox failure", nil)
	f.approve(p)
	broken := postgres.NewContentItemStore(f.pool, postgres.WithMaterialActivity(mergeMaterialTopic, ""),
		postgres.WithEditorialActivity("not-a-topic\x00"))
	input := domain.MergeChangeProposalInput{ProposalID: p.ID, ExpectedHeadID: p.Head.ID, ActorID: f.owner}
	if _, err := broken.MergeChangeProposal(t.Context(), input); err == nil {
		t.Fatal("accepted a merge that could not write its editorial events")
	}
	f.checkPublication(f.item.PublishedRevisionID)
	if count := countEditorial(t, f, mergeMaterialTopic); count != 1 {
		t.Fatal("catalog has a partial merge event", count)
	}
	if _, err := f.merge(p); err != nil {
		t.Fatal(err)
	}
	if count := countEditorial(t, f, topic); count != 3 {
		t.Fatal("unexpected editorial count after retry", count)
	}
}

func countEditorial(t *testing.T, f mergeFixture, topic string) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM outbox WHERE message_key = $1 AND topic = $2`,
		f.item.ID, topic).Scan(&count); err != nil {
		t.Fatal(err)
	}

	return count
}

func BenchmarkMergeEditorialActivity(b *testing.B) {
	for _, tc := range []struct{ name, topic string }{
		{"disabled", ""},
		{"enabled", "benchmark-" + uuid.NewString()},
	} {
		b.Run(tc.name, func(b *testing.B) {
			f := newMergeFixture(b, postgres.WithEditorialActivity(tc.topic))
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				b.StopTimer()
				p := f.proposal(uuid.NewString(), "# Benchmark change "+strconv.Itoa(i), nil)
				f.approve(p)
				b.StartTimer()
				merged, err := f.merge(p)
				if err != nil {
					b.Fatal(err)
				}
				f.item.PublishedRevisionID = merged.PublishedRevisionID
			}
		})
	}
}
