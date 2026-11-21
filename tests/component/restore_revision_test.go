//go:build component

package component

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/pkg/api/activity"
	"github.com/overmindv/content/tests/builders"
)

func TestRestorePublishedRevision(t *testing.T) {
	f := newActivityFixture(t)
	file := uuid.NewString()
	media, _, active := mediaBindingFixture(t, f.ctx, map[string]string{file: f.owner}, "")
	f.store = postgres.NewContentItemStore(f.pool, postgres.WithMaterialActivity(f.material, f.learning),
		postgres.WithManagedAttachments())
	bindings := service.NewMediaBindingWorker(f.store, media)
	deliver := func() {
		t.Helper()
		if found, err := bindings.DeliverNext(f.ctx); !found || err != nil {
			t.Fatal("attachment delivery", found, err)
		}
	}
	items := service.NewContentItemService(f.store, topicFixture(f.topic))
	items.SetAttachmentChecker(media)
	input := builders.ContentItem()
	input.CreatedBy, input.Slug = f.owner, input.Slug+"-"+uuid.NewString()
	input.Source = "# Original published content"
	input.Assets = []domain.CreateContentAssetInput{{AssetID: file, Kind: domain.AssetKindAttachment}}
	input.Learning = &domain.LearningMetadata{TopicIDs: []string{f.topic}}
	base, err := items.Create(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	deliver()
	base = f.state(t, base, domain.ContentStatusPublished)
	merged := publishWithoutBaseAttachments(t, f, base)

	draft, err := f.store.CreateRevision(f.ctx, domain.CreateContentRevisionInput{
		ContentItemID: base.ID, ExpectedRevisionID: merged, CreatedBy: f.owner,
		Format: domain.ContentFormatMarkdown, Source: "# Unpublished draft marker",
	})
	if err != nil {
		t.Fatal(err)
	}

	restore := domain.RestorePublishedRevisionInput{
		ContentItemID: base.ID, ExpectedPublishedID: merged, SourceRevisionID: base.PublishedRevisionID, ActorID: f.owner,
	}
	reject := func(actor, source string, want error) {
		t.Helper()
		attempt := restore
		attempt.ActorID, attempt.SourceRevisionID = actor, source
		if _, err := items.RestorePublishedRevision(f.ctx, attempt); !errors.Is(err, want) {
			t.Errorf("restore error = %v, want %v", err, want)
		}
	}
	reject(uuid.NewString(), base.PublishedRevisionID, service.ErrNotFound)
	reject(f.owner, draft.ID, service.ErrPublishedRevisionNotFound)
	reject(f.owner, merged, service.ErrInvalidState)
	other := f.state(t, f.create(t), domain.ContentStatusPublished)
	reject(f.owner, other.PublishedRevisionID, service.ErrPublishedRevisionNotFound)

	type result struct {
		revision domain.ContentRevision
		err      error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			revision, err := items.RestorePublishedRevision(f.ctx, restore)
			results <- result{revision, err}
		}()
	}
	var restored domain.ContentRevision
	var conflicts int
	for range 2 {
		r := <-results
		switch {
		case r.err == nil:
			restored = r.revision
		case errors.Is(r.err, service.ErrRevisionConflict):
			conflicts++
		default:
			t.Errorf("restore failed: %v", r.err)
		}
	}
	if conflicts != 1 || restored.ID == "" || restored.RestoredFromRevisionID != base.PublishedRevisionID ||
		restored.Source != input.Source || restored.Revision != draft.Revision+1 || restored.CreatedBy != f.owner {
		t.Fatal("restore did not create one new revision", restored.ID, conflicts)
	}
	owner, err := f.store.Get(f.ctx, base.ID, f.owner)
	if err != nil || owner.CurrentRevisionID != draft.ID || owner.PublishedRevisionID != restored.ID {
		t.Errorf("owner draft changed: %v", err)
	}
	reader, err := f.store.Get(f.ctx, base.ID, uuid.NewString())
	if err != nil || reader.CurrentRevisionID != restored.ID || reader.CurrentRevision == nil ||
		reader.CurrentRevision.RestoredFromRevisionID != base.PublishedRevisionID || len(reader.Assets) != 1 || reader.Assets[0].AssetID != file {
		t.Errorf("reader did not get the restored revision and attachments: %v", err)
	}
	revisions, err := f.store.ListRevisions(f.ctx, base.ID, f.owner)
	if err != nil || len(revisions) != 4 || revisions[0].RestoredFromRevisionID != base.PublishedRevisionID {
		t.Errorf("restore history has %d revisions: %v", len(revisions), err)
	}
	history, err := f.store.ListRevisionHistory(f.ctx, domain.RevisionHistoryInput{
		ContentItemID: base.ID, ActorID: f.owner, PageSize: 1,
	})
	if err != nil || len(history) != 2 || history[0].ID != restored.ID ||
		history[0].RestoredFromRevisionID != base.PublishedRevisionID || history[1].ID != draft.ID {
		t.Error("paged history lost the restoration source or draft", err)
	}
	events := readActivity[activity.MaterialState](t, f, f.material)
	if len(events) != 4 || events[3].RevisionID != restored.ID || events[3].EventType != activity.MaterialCorrected ||
		events[3].AggregateVersion <= events[1].AggregateVersion || events[3].Payload.Tombstone ||
		events[3].Payload.InvalidatedRevisionID != merged {
		t.Error("restore did not advance the public material stream exactly once")
	}
	deliver()
	if count := active(file); count != 2 {
		t.Errorf("file should remain bound to both publications, got %d references", count)
	}
	if _, err = f.store.Delete(f.ctx, base.ID, f.owner); err != nil {
		t.Fatal(err)
	}
	deliver()
	deliver()
	if count := active(file); count != 0 {
		t.Errorf("deletion left %d active references", count)
	}
}

func publishWithoutBaseAttachments(t *testing.T, f activityFixture, base domain.ContentItem) string {
	t.Helper()
	peer := uuid.NewString()
	proposal, err := f.store.OpenChangeProposal(f.ctx, domain.OpenChangeProposalInput{
		ContentItemID: base.ID, BaseRevisionID: base.PublishedRevisionID, AuthorID: uuid.NewString(),
		Source: "# Updated material", Attachments: &domain.AttachmentSet{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.AssignReviewers(f.ctx, domain.ReviewAssignmentRequest{
		ProposalID: proposal.ID, RevisionID: proposal.Head.ID, TopicID: f.topic, RankedCandidateIDs: []string{f.owner, peer},
	}); err != nil {
		t.Fatal(err)
	}
	for _, reviewer := range []string{f.owner, peer} {
		if _, err = f.store.SubmitReviewDecision(f.ctx, domain.SubmitReviewDecisionInput{
			ReviewRevisionRef: domain.ReviewRevisionRef{
				ReviewActor: domain.ReviewActor{ProposalID: proposal.ID, ActorID: reviewer}, RevisionID: proposal.Head.ID,
			},
			Decision: domain.ReviewApprove,
		}); err != nil {
			t.Fatal(err)
		}
	}
	merged, err := f.store.MergeChangeProposal(f.ctx, domain.MergeChangeProposalInput{
		ProposalID: proposal.ID, ExpectedHeadID: proposal.Head.ID, ActorID: f.owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	return merged.PublishedRevisionID
}
