//go:build component

package component

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
)

func TestInitialPublicationRequiresFreshReview(t *testing.T) {
	f := newDraftReviewFixture(t, postgres.WithReviewCheckScheduling())
	ctx := t.Context()
	request := domain.SubmitInitialPublicationInput{ContentItemID: f.item.ID, ExpectedRevisionID: f.item.CurrentRevisionID, AuthorID: f.owner}
	foreign := request
	foreign.AuthorID = uuid.NewString()
	_, err := f.store.SubmitInitialPublication(ctx, foreign)
	assertPublicationError(t, err, service.ErrNotFound)

	first, err := f.store.SubmitInitialPublication(ctx, request)
	if err != nil || !first.InitialPublication || first.BaseOutdated || first.BaseRevisionID != request.ExpectedRevisionID {
		t.Fatal("submission did not pin the draft:", first, err)
	}
	again, err := f.store.SubmitInitialPublication(ctx, request)
	if err != nil || again.ID != first.ID || again.Head.ID != first.Head.ID {
		t.Fatal("repeat changed the review:", again, err)
	}
	_, err = f.store.Get(ctx, f.item.ID, foreign.AuthorID)
	assertPublicationError(t, err, service.ErrNotFound)
	_, err = f.store.GetChangeProposal(ctx, first.ID, foreign.AuthorID)
	assertPublicationError(t, err, service.ErrProposalNotFound)
	_, err = f.merge(first)
	assertPublicationError(t, err, service.ErrReviewRequired)
	_, err = f.store.ChangeState(ctx, domain.ChangeContentItemStateInput{
		ID: f.item.ID, ActorID: f.owner, ExpectedRevisionID: request.ExpectedRevisionID,
	}, domain.ContentStatusPublished)
	assertPublicationError(t, err, service.ErrReviewRequired)
	_, err = f.store.AppendChangeProposalRevision(ctx, domain.AppendChangeProposalRevisionInput{
		ProposalID: first.ID, ExpectedHeadID: first.Head.ID, AuthorID: f.owner, Source: "# Not the reviewed draft",
	})
	assertPublicationError(t, err, service.ErrInvalidState)

	if _, err = f.pool.Exec(ctx, `
		UPDATE content_change_proposal_revisions SET review_due_at = '2000-01-01T00:00:00Z'
		WHERE proposal_id = $1 AND id = $2`, first.ID, first.Head.ID); err != nil {
		t.Fatal(err)
	}
	check, found, err := f.store.ClaimReviewCheck(ctx)
	if err != nil || !found || check.ProposalID != first.ID || check.RevisionID != first.Head.ID || check.TopicID != f.topic {
		t.Fatal("review worker did not claim the draft:", check, found, err)
	}
	reviewers := []string{uuid.NewString(), uuid.NewString()}
	if err = f.store.ApplyReviewCheck(ctx, check, domain.ReviewCandidates{
		RankedIDs: append([]string{f.owner}, reviewers...),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = f.store.SubmitReviewDecision(ctx, initialVote(first, f.owner))
	assertPublicationError(t, err, service.ErrReviewNotAllowed)
	for _, reviewer := range reviewers {
		visible, readErr := f.store.GetChangeProposal(ctx, first.ID, reviewer)
		if readErr != nil || visible.Head.ID != first.Head.ID {
			t.Fatal("assigned reviewer cannot read the proposal:", readErr)
		}
		if _, voteErr := f.store.SubmitReviewDecision(ctx, initialVote(first, reviewer)); voteErr != nil {
			t.Fatal(voteErr)
		}
	}

	const editedSource = "# New draft after review"
	saved, err := f.store.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID: f.item.ID, ExpectedRevisionID: request.ExpectedRevisionID,
		Format: domain.ContentFormatMarkdown, Source: editedSource, CreatedBy: f.owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.merge(first)
	assertPublicationError(t, err, service.ErrInvalidState)
	_, err = f.store.GetChangeProposal(ctx, first.ID, reviewers[0])
	assertPublicationError(t, err, service.ErrProposalNotFound)
	_, err = f.store.SubmitInitialPublication(ctx, request)
	assertPublicationError(t, err, service.ErrRevisionConflict)

	request.ExpectedRevisionID = saved.ID
	second, err := f.store.SubmitInitialPublication(ctx, request)
	if err != nil || second.ID == first.ID || second.Head.Source != editedSource {
		t.Fatal("updated draft reused the old review:", second, err)
	}
	_, err = f.merge(second)
	assertPublicationError(t, err, service.ErrReviewRequired)
	approveInitialDraft(t, f, second, reviewers)
	merged, err := f.merge(second)
	if err != nil || merged.PublishedRevisionID != saved.ID {
		t.Fatal("approved draft was not published:", merged, err)
	}
	f.checkPublication(saved.ID)
	published, err := f.store.Get(ctx, f.item.ID, foreign.AuthorID)
	if err != nil || published.CurrentRevision == nil || published.CurrentRevision.Source != editedSource {
		t.Fatal("reader did not get the approved text:", err)
	}
	retryMerge, err := f.merge(second)
	if err != nil || retryMerge != merged {
		t.Fatal("merge retry did not return the published revision:", retryMerge, err)
	}
	retrySubmit, err := f.store.SubmitInitialPublication(ctx, request)
	if err != nil || retrySubmit.ID != second.ID || retrySubmit.Status != domain.ChangeProposalMerged {
		t.Fatal("submit retry after publication opened a new review:", retrySubmit, err)
	}
	var publishedEvents int
	err = f.pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE topic = $1 AND message_key = $2`, mergeMaterialTopic, f.item.ID).Scan(&publishedEvents)
	if err != nil || publishedEvents != 1 {
		t.Fatal("first publication must emit one material event:", publishedEvents, err)
	}
}

func TestInitialPublicationKeepsApprovedAttachments(t *testing.T) {
	f := newDraftReviewFixture(t, postgres.WithManagedAttachments())
	ctx := t.Context()
	fileID := uuid.NewString()
	saved, err := f.store.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID: f.item.ID, ExpectedRevisionID: f.item.CurrentRevisionID,
		Format: domain.ContentFormatMarkdown, Source: "# Article with a file", CreatedBy: f.owner,
		Attachments: &domain.AttachmentSet{Assets: []domain.CreateContentAssetInput{
			{AssetID: fileID, Kind: domain.AssetKindAttachment},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := domain.SubmitInitialPublicationInput{ContentItemID: f.item.ID, ExpectedRevisionID: saved.ID, AuthorID: f.owner}
	_, err = f.store.SubmitInitialPublication(ctx, request)
	assertPublicationError(t, err, service.ErrAttachmentsPending)
	if _, err = f.pool.Exec(ctx, `
		UPDATE content_media_bindings SET applied_version = desired_version WHERE revision_id = $1`, saved.ID); err != nil {
		t.Fatal(err)
	}
	first, err := f.store.SubmitInitialPublication(ctx, request)
	if err != nil || len(first.Head.Assets) != 1 || first.Head.Assets[0].AssetID != fileID {
		t.Fatal("submission lost its attachment:", first.Head.Assets, err)
	}
	if _, err = f.store.Update(ctx, domain.UpdateContentItemInput{
		ID: f.item.ID, Type: f.item.Type, Title: "Updated title", Slug: f.item.Slug, UpdatedBy: f.owner,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = f.merge(first)
	assertPublicationError(t, err, service.ErrInvalidState)

	second, err := f.store.SubmitInitialPublication(ctx, request)
	if err != nil || second.ID == first.ID || len(second.Head.Assets) != 1 || second.Head.Assets[0].AssetID != fileID {
		t.Fatal("metadata change did not open a fresh review:", second, err)
	}
	approveInitialDraft(t, f, second, []string{uuid.NewString(), uuid.NewString()})
	if _, err = f.merge(second); err != nil {
		t.Fatal(err)
	}
	item, err := f.store.Get(ctx, f.item.ID, "")
	if err != nil || len(item.Assets) != 1 || item.Assets[0].AssetID != fileID || item.PublishedRevisionID != saved.ID {
		t.Fatal("published attachments differ from the approved revision:", item.Assets, err)
	}
	var desired, applied int
	err = f.pool.QueryRow(ctx, `SELECT desired_version, applied_version FROM content_media_bindings WHERE revision_id = $1`,
		saved.ID).Scan(&desired, &applied)
	if err != nil || desired != 1 || applied != 1 {
		t.Fatal("publishing retired the approved draft binding:", desired, applied, err)
	}
}

func initialVote(p domain.ChangeProposal, reviewer string) domain.SubmitReviewDecisionInput {
	return domain.SubmitReviewDecisionInput{
		ReviewRevisionRef: domain.ReviewRevisionRef{ReviewActor: domain.ReviewActor{ProposalID: p.ID, ActorID: reviewer}, RevisionID: p.Head.ID},
		Decision:          domain.ReviewApprove,
	}
}

func approveInitialDraft(t *testing.T, f mergeFixture, p domain.ChangeProposal, reviewers []string) {
	t.Helper()
	ctx := t.Context()
	if err := f.store.AssignReviewers(ctx, domain.ReviewAssignmentRequest{
		ProposalID: p.ID, RevisionID: p.Head.ID, TopicID: f.topic, RankedCandidateIDs: reviewers,
	}); err != nil {
		t.Fatal(err)
	}
	for _, reviewer := range reviewers {
		if _, err := f.store.SubmitReviewDecision(ctx, initialVote(p, reviewer)); err != nil {
			t.Fatal(err)
		}
	}
}

func assertPublicationError(t *testing.T, got, expected error) {
	t.Helper()
	if !errors.Is(got, expected) {
		t.Fatal("expected", expected, "received", got)
	}
}
