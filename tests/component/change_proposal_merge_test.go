//go:build component

package component

import (
	"context"
	"encoding/json"
	"errors"
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

const mergeMaterialTopic = "fixture.material.merge"

type mergeFixture struct {
	t            *testing.T
	pool         *pgxpool.Pool
	store        *postgres.ContentItemStore
	item         domain.ContentItem
	owner, topic string
}

func newMergeFixture(t *testing.T, options ...postgres.StoreOption) mergeFixture {
	t.Helper()
	cfg := testDatabaseConfig(t)
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	options = append([]postgres.StoreOption{
		postgres.WithOutbox(true, "fixture.legacy.merge"),
		postgres.WithMaterialActivity(mergeMaterialTopic, ""),
	}, options...)
	f := mergeFixture{t: t, pool: pool, store: postgres.NewContentItemStore(pool, options...), owner: uuid.NewString(), topic: uuid.NewString()}
	input := builders.ContentItem()
	input.CreatedBy, input.Slug = f.owner, uuid.NewString()
	input.Learning = &domain.LearningMetadata{TopicIDs: []string{f.topic}}
	f.item, err = f.store.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	itemID := f.item.ID
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if _, deleteErr := f.store.Delete(ctx, itemID, f.owner); deleteErr != nil {
			t.Error(deleteErr)
		}
		if _, cleanupErr := pool.Exec(ctx, `DELETE FROM content_media_bindings WHERE material_id = $1`, itemID); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	state := domain.ChangeContentItemStateInput{ID: f.item.ID, ExpectedRevisionID: f.item.CurrentRevisionID, ActorID: f.owner}
	f.item, err = f.store.ChangeState(t.Context(), state, domain.ContentStatusPublished)
	if err != nil {
		t.Fatal(err)
	}

	return f
}

func (f mergeFixture) proposal(author, source string, attachments *domain.AttachmentSet) domain.ChangeProposal {
	f.t.Helper()
	p, err := f.store.OpenChangeProposal(f.t.Context(), domain.OpenChangeProposalInput{
		ContentItemID: f.item.ID, BaseRevisionID: f.item.PublishedRevisionID, AuthorID: author, Source: source, Attachments: attachments,
	})
	if err != nil {
		f.t.Fatal(err)
	}

	return p
}

func (f mergeFixture) approve(p domain.ChangeProposal) string {
	f.t.Helper()
	other := uuid.NewString()
	if err := f.store.AssignReviewers(f.t.Context(), domain.ReviewAssignmentRequest{
		ProposalID: p.ID, RevisionID: p.Head.ID, TopicID: f.topic, RankedCandidateIDs: []string{f.owner, other},
	}); err != nil {
		f.t.Fatal(err)
	}
	for _, actor := range []string{f.owner, other} {
		if _, err := f.store.SubmitReviewDecision(f.t.Context(), domain.SubmitReviewDecisionInput{
			ReviewRevisionRef: domain.ReviewRevisionRef{
				ReviewActor: domain.ReviewActor{ProposalID: p.ID, ActorID: actor}, RevisionID: p.Head.ID,
			},
			Decision: domain.ReviewApprove,
		}); err != nil {
			f.t.Fatal(err)
		}
	}

	return other
}

func (f mergeFixture) merge(p domain.ChangeProposal) (domain.MergeChangeProposalResult, error) {
	return f.store.MergeChangeProposal(f.t.Context(), domain.MergeChangeProposalInput{
		ProposalID: p.ID, ExpectedHeadID: p.Head.ID, ActorID: f.owner,
	})
}

func (f mergeFixture) checkPublication(id string) {
	f.t.Helper()
	published, err := f.store.Get(f.t.Context(), f.item.ID, "")
	if err != nil || published.PublishedRevisionID != id || published.CurrentRevisionID != id {
		f.t.Fatal("unexpected public revision", err, published.PublishedRevisionID)
	}
}

func TestMergeCompetingProposals(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	draft, err := f.store.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID: f.item.ID, ExpectedRevisionID: f.item.CurrentRevisionID,
		Source: "# Private draft", Format: domain.ContentFormatMarkdown, CreatedBy: f.owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := f.proposal(uuid.NewString(), "# First proposal", nil)
	second := f.proposal(uuid.NewString(), "# Second proposal", nil)
	f.approve(first)
	f.approve(second)
	if _, mergeErr := f.store.MergeChangeProposal(ctx, domain.MergeChangeProposalInput{
		ProposalID: first.ID, ExpectedHeadID: first.Head.ID, ActorID: first.AuthorID,
	}); !errors.Is(mergeErr, service.ErrProposalNotFound) {
		t.Fatal("non-owner could accept a proposal", mergeErr)
	}
	type outcome struct {
		p   domain.ChangeProposal
		id  string
		err error
	}
	out := make(chan outcome, 2)
	start := make(chan struct{})
	for _, p := range []domain.ChangeProposal{first, second} {
		go func() {
			<-start
			m, mergeErr := f.merge(p)
			out <- outcome{p: p, id: m.PublishedRevisionID, err: mergeErr}
		}()
	}
	close(start)
	var winner outcome
	conflicts := 0
	for range 2 {
		r := <-out
		switch {
		case r.err == nil && winner.id == "":
			winner = r
		case errors.Is(r.err, service.ErrMergeConflict):
			conflicts++
		default:
			t.Fatal("unexpected merge result", r.err)
		}
	}
	if winner.id == "" || conflicts != 1 {
		t.Fatal("expected exactly one publication and one conflict")
	}
	f.checkPublication(winner.id)
	ownerView, err := f.store.Get(ctx, f.item.ID, f.owner)
	if err != nil || ownerView.CurrentRevisionID != draft.ID {
		t.Fatal("merge overwrote the owner's draft", err)
	}
	again, err := f.merge(winner.p)
	if err != nil || again.PublishedRevisionID != winner.id {
		t.Fatal("merge retry changed the result", err)
	}
	var count int
	var body []byte
	err = f.pool.QueryRow(ctx, `
		SELECT count(*), (array_agg(message_value ORDER BY created_at DESC))[1]
		FROM outbox WHERE message_key = $1 AND topic = $2`, f.item.ID, mergeMaterialTopic).Scan(&count, &body)
	var event activity.Event[activity.MaterialState]
	if err != nil || count != 2 || json.Unmarshal(body, &event) != nil || event.RevisionID != winner.id {
		t.Fatal("expected a single public event for the merge", err, count)
	}
	accepted, err := f.store.GetChangeProposal(ctx, winner.p.ID, f.owner)
	if err != nil || accepted.Status != domain.ChangeProposalMerged || accepted.MergedRevisionID != winner.id {
		t.Fatal("merge did not close the proposal", err)
	}
}

func TestMergeRollsBackWhenPublicEventFails(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	p := f.proposal(uuid.NewString(), "# Review for rollback", nil)
	f.approve(p)
	failing := postgres.NewContentItemStore(f.pool, postgres.WithMaterialActivity("fixture.blocked.merge", ""))
	if _, err := f.pool.Exec(ctx, `ALTER TABLE outbox ADD CONSTRAINT fixture_merge_event_rejected
		CHECK (topic <> 'fixture.blocked.merge') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if _, err := f.pool.Exec(deadline, `ALTER TABLE outbox DROP CONSTRAINT fixture_merge_event_rejected`); err != nil {
			t.Error(err)
		}
	})
	input := domain.MergeChangeProposalInput{ProposalID: p.ID, ExpectedHeadID: p.Head.ID, ActorID: f.owner}
	if result, err := failing.MergeChangeProposal(ctx, input); err == nil || result.PublishedRevisionID != "" {
		t.Fatal("merge did not roll back on an outbox error", err)
	}
	f.checkPublication(f.item.PublishedRevisionID)
	got, err := f.store.GetChangeProposal(ctx, p.ID, f.owner)
	if err != nil || got.Status != domain.ChangeProposalOpen || got.MergedRevisionID != "" {
		t.Fatal("rollback left the proposal closed", err)
	}
	revisions, err := f.store.ListRevisions(ctx, f.item.ID, f.owner)
	if err != nil || len(revisions) != 1 {
		t.Fatal("rollback left a new revision", len(revisions), err)
	}
	if _, err = f.merge(p); err != nil {
		t.Fatal("merge did not recover after a transient outbox error", err)
	}
}

func TestMergeWaitsForAttachmentBindingAndRechecksReview(t *testing.T) {
	f := newMergeFixture(t, postgres.WithManagedAttachments())
	ctx := t.Context()
	author, fileID := uuid.NewString(), uuid.NewString()
	media, unavailable, active := mediaBindingFixture(t, ctx, map[string]string{fileID: author}, "")
	p := f.proposal(author, "# Proposal with an attachment", &domain.AttachmentSet{
		Assets: []domain.CreateContentAssetInput{{AssetID: fileID, Kind: domain.AssetKindAttachment}},
	})
	if _, err := f.merge(p); !errors.Is(err, service.ErrReviewRequired) {
		t.Fatal("unreviewed proposal was accepted", err)
	}
	peer := f.approve(p)
	thread, err := f.store.CreateReviewThread(ctx, domain.CreateReviewThreadInput{
		ReviewActor: domain.ReviewActor{ProposalID: p.ID, ActorID: peer},
		RevisionID:  p.Head.ID, Body: "Check the attachment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, mergeErr := f.merge(p); !errors.Is(mergeErr, service.ErrUnresolvedReviewThreads) {
		t.Fatal("open discussion did not block merge", mergeErr)
	}
	if _, err = f.store.SetReviewThreadResolved(ctx, domain.ReviewThreadRef{
		ReviewActor: domain.ReviewActor{ProposalID: p.ID, ActorID: peer}, ThreadID: thread.ID,
	}, true); err != nil {
		t.Fatal(err)
	}
	worker := service.NewMediaBindingWorker(f.store, media)
	deliver := func() {
		t.Helper()
		if found, deliveryErr := worker.DeliverNext(ctx); !found || deliveryErr != nil {
			t.Fatal("binding was not delivered", found, deliveryErr)
		}
	}
	deliver()
	pending := func() {
		t.Helper()
		if _, mergeErr := f.merge(p); !errors.Is(mergeErr, service.ErrAttachmentsPending) {
			t.Fatal("unconfirmed binding did not block merge", mergeErr)
		}
		f.checkPublication(f.item.PublishedRevisionID)
	}
	candidate := func() string {
		t.Helper()
		var id string
		if scanErr := f.pool.QueryRow(ctx, `SELECT revision_id FROM content_proposal_merge_candidates WHERE proposal_id = $1`, p.ID).
			Scan(&id); scanErr != nil {
			t.Fatal(scanErr)
		}

		return id
	}
	pending()
	oldCandidate := candidate()
	pending()
	if candidate() != oldCandidate {
		t.Fatal("retry queued a duplicate binding")
	}
	revisions, err := f.store.ListRevisions(ctx, f.item.ID, f.owner)
	if err != nil || len(revisions) != 1 {
		t.Fatal("pending publication appeared in the material history", err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE content_proposal_merge_candidates
		SET expires_at = statement_timestamp() - INTERVAL '1 second' WHERE proposal_id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.DeliverNext(ctx); err != nil {
		t.Fatal(err)
	}
	var retired int
	if err = f.pool.QueryRow(ctx, `SELECT desired_version FROM content_media_bindings WHERE revision_id = $1`,
		oldCandidate).Scan(&retired); err != nil || retired != 2 {
		t.Fatal("expired binding was not retired", err)
	}
	deliver()
	pending()
	readyCandidate := candidate()
	if readyCandidate == oldCandidate {
		t.Fatal("expired binding was reused")
	}
	unavailable.Store(true)
	if found, deliveryErr := worker.DeliverNext(ctx); !found || !errors.Is(deliveryErr, service.ErrMediaUnavailable) {
		t.Fatal("expected a transient Media failure", found, deliveryErr)
	}
	pending()
	unavailable.Store(false)
	if _, err = f.pool.Exec(ctx, `UPDATE content_media_bindings SET next_attempt_at = statement_timestamp()
		WHERE revision_id = $1`, readyCandidate); err != nil {
		t.Fatal(err)
	}
	deliver()
	if err = f.store.RevokeReviewAssignment(ctx, p.ID, p.Head.ID, peer); err != nil {
		t.Fatal(err)
	}
	if _, mergeErr := f.merge(p); !errors.Is(mergeErr, service.ErrReviewRequired) {
		t.Fatal("merge used a revoked vote", mergeErr)
	}
	f.checkPublication(f.item.PublishedRevisionID)
	f.approve(p)
	merged, err := f.merge(p)
	if err != nil || merged.PublishedRevisionID != readyCandidate {
		t.Fatal("merge did not promote the bound revision", err)
	}
	deliver()
	if active(fileID) != 1 {
		t.Fatal("merge did not keep exactly one active binding for the published file")
	}
	ref := domain.PublishedAttachmentRef{MaterialID: f.item.ID, RevisionID: merged.PublishedRevisionID, FileID: fileID}
	if err = f.store.CheckPublishedAttachment(ctx, ref); err != nil {
		t.Fatal("attachment is not accessible from the merged version", err)
	}
}
