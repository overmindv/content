//go:build component

package component

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/internal/pkg/validator"
	"github.com/overmindv/content/tests/builders"
)

func TestChangeProposalLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(t))
	if err != nil {
		t.Fatal("could not open test database")
	}
	defer pool.Close()
	store := postgres.NewContentItemStore(pool, postgres.WithOutbox(true, "content-test-topic"))
	owner, author, reader := uuid.NewString(), uuid.NewString(), uuid.NewString()
	input := builders.ContentItem()
	input.CreatedBy = owner
	input.Slug += "-" + uuid.NewString()
	item, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	open := domain.OpenChangeProposalInput{
		ContentItemID: item.ID, BaseRevisionID: item.CurrentRevisionID, AuthorID: author, Source: "# Proposed material",
	}
	if _, err = store.OpenChangeProposal(ctx, open); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("open on unpublished material: %v", err)
	}
	state := domain.ChangeContentItemStateInput{ID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, ActorID: owner}
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusPublished); err != nil {
		t.Fatal(err)
	}
	unchanged := open
	unchanged.Source = input.Source
	if _, err = store.OpenChangeProposal(ctx, unchanged); !errors.Is(err, service.ErrProposalNoChange) {
		t.Error("opened a proposal without changes", err)
	}
	storeDraft, err := store.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID: item.ID, ExpectedRevisionID: item.CurrentRevisionID,
		Source: "# Unpublished author's draft", Format: domain.ContentFormatMarkdown, CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongBase := open
	wrongBase.BaseRevisionID = storeDraft.ID
	if _, err = store.OpenChangeProposal(ctx, wrongBase); !errors.Is(err, service.ErrRevisionConflict) {
		t.Errorf("open from draft rather than published revision: %v", err)
	}

	proposal := raceProposalOperation(t, service.ErrProposalExists, func(_ int) (domain.ChangeProposal, error) {
		return store.OpenChangeProposal(ctx, open)
	})
	if proposal.BaseRevisionID != item.CurrentRevisionID || proposal.BaseOutdated || proposal.Head.Ordinal != 1 || proposal.Head.ParentRevisionID != "" {
		t.Error("first revision is not based on the published material")
	}
	if _, err = store.GetChangeProposal(ctx, proposal.ID, reader); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("get as unrelated reader: %v", err)
	}
	for _, participant := range []string{owner, author} {
		if _, err = store.GetChangeProposal(ctx, proposal.ID, participant); err != nil {
			t.Errorf("get as participant: %v", err)
		}
	}
	appendRevision := domain.AppendChangeProposalRevisionInput{
		ProposalID: proposal.ID, ExpectedHeadID: proposal.Head.ID, AuthorID: owner, Source: "# Updated proposal",
	}
	if _, err = store.AppendChangeProposalRevision(ctx, appendRevision); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("append as material owner: %v", err)
	}
	if _, err = store.CloseChangeProposal(ctx, proposal.ID, owner); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("close as material owner: %v", err)
	}
	appendRevision.AuthorID = author
	unchangedHead := appendRevision
	unchangedHead.Source = open.Source
	if _, err = store.AppendChangeProposalRevision(ctx, unchangedHead); !errors.Is(err, service.ErrProposalNoChange) {
		t.Error("created a proposal revision without changes", err)
	}
	updated := raceProposalOperation(t, service.ErrRevisionConflict, func(n int) (domain.ChangeProposal, error) {
		edit := appendRevision
		if n == 1 {
			edit.Source = "# Another update"
		}
		return store.AppendChangeProposalRevision(ctx, edit)
	})
	if updated.Head.Ordinal != 2 || updated.Head.ParentRevisionID != proposal.Head.ID || updated.Head.ID == proposal.Head.ID {
		t.Error("append did not create a new head linked to the original revision")
	}
	var originalSource string
	if err = pool.QueryRow(ctx, `SELECT source FROM content_change_proposal_revisions WHERE proposal_id = $1 AND id = $2`, proposal.ID, proposal.Head.ID).Scan(&originalSource); err != nil || originalSource != open.Source {
		t.Errorf("original revision was not preserved: %v", err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM content_change_proposal_revisions WHERE proposal_id = $1`, proposal.ID).Scan(&count); err != nil || count != 2 {
		t.Errorf("proposal revision count = %d, want 2: %v", count, err)
	}
	t.Run("history and diff", func(t *testing.T) {
		readerInput := domain.ChangeProposalHistoryInput{ProposalID: proposal.ID, ActorID: owner, PageSize: 1}
		first, err := store.ListChangeProposalHistory(ctx, readerInput)
		if err != nil || len(first.Entries) != 1 || first.NextBeforeOrdinal != 2 {
			t.Fatalf("first history page: %+v, %v", first, err)
		}
		entry := first.Entries[0]
		if entry.ID != updated.Head.ID || entry.ParentRevisionID != proposal.Head.ID || entry.BaseRevisionID != proposal.BaseRevisionID ||
			entry.AuthorID != author || entry.CreatedAt.IsZero() {
			t.Error("history entry is missing revision metadata")
		}
		readerInput.BeforeOrdinal = first.NextBeforeOrdinal
		second, err := store.ListChangeProposalHistory(ctx, readerInput)
		if err != nil || second.NextBeforeOrdinal != 0 || len(second.Entries) != 1 || second.Entries[0].ID != proposal.Head.ID {
			t.Fatalf("second history page: %+v, %v", second, err)
		}
		readerInput.BeforeOrdinal = 1
		last, err := store.ListChangeProposalHistory(ctx, readerInput)
		if err != nil || len(last.Entries) != 0 || last.NextBeforeOrdinal != 0 {
			t.Errorf("history past last entry: %+v, %v", last, err)
		}
		readerInput.ActorID = reader
		if _, err = store.ListChangeProposalHistory(ctx, readerInput); !errors.Is(err, service.ErrProposalNotFound) {
			t.Errorf("history as unrelated reader: %v", err)
		}

		diffInput := domain.ChangeProposalDiffInput{ProposalID: proposal.ID, ActorID: author}
		sources, err := store.GetChangeProposalDiffSources(ctx, diffInput)
		if err != nil || sources.RevisionID != updated.Head.ID || sources.BaseSource != input.Source || sources.Source != updated.Head.Source {
			t.Errorf("diff against current head used the wrong sources: %v", err)
		}
		diffInput.RevisionID = proposal.Head.ID
		sources, err = store.GetChangeProposalDiffSources(ctx, diffInput)
		if err != nil || sources.Source != open.Source || sources.RevisionID != proposal.Head.ID {
			t.Errorf("diff against the first revision used the wrong source: %v", err)
		}
		diffInput.RevisionID = storeDraft.ID
		if _, err = store.GetChangeProposalDiffSources(ctx, diffInput); !errors.Is(err, service.ErrProposalRevisionNotFound) {
			t.Errorf("diff against a revision outside the proposal: %v", err)
		}
		diffInput.ActorID, diffInput.RevisionID = reader, proposal.Head.ID
		if _, err = store.GetChangeProposalDiffSources(ctx, diffInput); !errors.Is(err, service.ErrProposalNotFound) {
			t.Errorf("diff as unrelated reader: %v", err)
		}
		if _, err = pool.Exec(ctx, `UPDATE content_revisions SET source = $2 WHERE id = $1`, item.CurrentRevisionID, strings.Repeat("x", validator.MaxSourceBytes+1)); err != nil {
			t.Fatal(err)
		}
		diffInput.ActorID = author
		sources, err = store.GetChangeProposalDiffSources(ctx, diffInput)
		if err != nil || !sources.Oversized || sources.BaseSource != "" {
			t.Errorf("oversized base source was not withheld: %v", err)
		}
	})
	closed, err := store.CloseChangeProposal(ctx, proposal.ID, author)
	if err != nil || closed.Status != domain.ChangeProposalClosed || closed.ClosedAt == nil {
		t.Fatalf("close proposal: %v", err)
	}
	repeated, err := store.CloseChangeProposal(ctx, proposal.ID, author)
	if err != nil || repeated.ClosedAt == nil || !repeated.ClosedAt.Equal(*closed.ClosedAt) {
		t.Errorf("repeated close changed the result: %v", err)
	}
	appendRevision.ExpectedHeadID = updated.Head.ID
	if _, err = store.AppendChangeProposalRevision(ctx, appendRevision); !errors.Is(err, service.ErrInvalidState) {
		t.Errorf("append after close: %v", err)
	}
	reopened, err := store.OpenChangeProposal(ctx, open)
	if err != nil || reopened.ID == proposal.ID {
		t.Errorf("open a new proposal after closing: %v", err)
	}
	publicView, err := store.Get(ctx, item.ID, author)
	if err != nil || publicView.CurrentRevisionID != item.CurrentRevisionID {
		t.Errorf("proposal changed the published material: %v", err)
	}
	ownerView, err := store.Get(ctx, item.ID, owner)
	if err != nil || ownerView.CurrentRevisionID != storeDraft.ID {
		t.Errorf("proposal changed the owner's draft: %v", err)
	}
	if n := pendingOutboxRows(t, ctx, pool, item.ID); n != 3 {
		t.Errorf("outbox rows = %d, want only the 3 material events", n)
	}
	state.ExpectedRevisionID = storeDraft.ID
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusPublished); err != nil {
		t.Fatal(err)
	}
	outdated, err := store.GetChangeProposal(ctx, reopened.ID, author)
	if err != nil || !outdated.BaseOutdated || outdated.BaseRevisionID != item.CurrentRevisionID {
		t.Error("new publication did not mark the old base without changing it", err)
	}
	if _, err = store.Delete(ctx, item.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetChangeProposal(ctx, proposal.ID, author); !errors.Is(err, service.ErrProposalNotFound) {
		t.Errorf("proposal survived material deletion: %v", err)
	}
}

func raceProposalOperation(t *testing.T, rejected error, op func(int) (domain.ChangeProposal, error)) domain.ChangeProposal {
	t.Helper()
	type result struct {
		proposal domain.ChangeProposal
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i := range 2 {
		go func() {
			<-start
			p, err := op(i)
			results <- result{proposal: p, err: err}
		}()
	}
	close(start)
	var winner domain.ChangeProposal
	var accepted, rejectedCount int
	for range 2 {
		r := <-results
		switch {
		case r.err == nil:
			winner = r.proposal
			accepted++
		case errors.Is(r.err, rejected):
			rejectedCount++
		default:
			t.Errorf("unexpected concurrent result: %v", r.err)
		}
	}
	if accepted != 1 || rejectedCount != 1 {
		t.Fatalf("accepted = %d, rejected = %d; want one of each", accepted, rejectedCount)
	}
	return winner
}
