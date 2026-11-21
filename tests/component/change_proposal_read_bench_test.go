//go:build component

package component

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/tests/builders"
)

func BenchmarkProposalRead(b *testing.B) {
	ctx := b.Context()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(b))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(pool.Close)
	store := postgres.NewContentItemStore(pool)
	input := builders.ContentItem()
	input.Slug, input.CreatedBy = uuid.NewString(), uuid.NewString()
	item, err := store.Create(ctx, input)
	if err != nil {
		b.Fatal(err)
	}
	state := domain.ChangeContentItemStateInput{ID: item.ID, ActorID: input.CreatedBy, ExpectedRevisionID: item.CurrentRevisionID}
	if _, publishErr := store.ChangeState(ctx, state, domain.ContentStatusPublished); publishErr != nil {
		b.Fatal(publishErr)
	}
	author := uuid.NewString()
	proposal, err := store.OpenChangeProposal(ctx, domain.OpenChangeProposalInput{
		ContentItemID: item.ID, BaseRevisionID: item.CurrentRevisionID, AuthorID: author, Source: "# Suggested wording",
	})
	if err != nil {
		b.Fatal(err)
	}
	if _, seedErr := pool.Exec(ctx, `
		WITH ids AS (
		    SELECT n, gen_random_uuid() AS id FROM generate_series(2, 10000) AS n
		), revisions AS (
		    INSERT INTO content_change_proposal_revisions (proposal_id, id, parent_revision_id, ordinal, source, message)
		    SELECT $1, id, lag(id, 1, $2::uuid) OVER (ORDER BY n), n, '# Suggested wording', 'edit' FROM ids
		    RETURNING id, ordinal
		)
		UPDATE content_change_proposals SET head_revision_id = (SELECT id FROM revisions WHERE ordinal = 10000)
		WHERE id = $1`, proposal.ID, proposal.Head.ID); seedErr != nil {
		b.Fatal(seedErr)
	}
	b.Run("history", func(b *testing.B) {
		query := domain.ChangeProposalHistoryInput{ProposalID: proposal.ID, ActorID: author, BeforeOrdinal: 5000, PageSize: 50}
		b.ReportAllocs()
		for b.Loop() {
			page, err := store.ListChangeProposalHistory(b.Context(), query)
			if err != nil || len(page.Entries) != 50 {
				b.Fatal("unexpected history page", err)
			}
		}
	})
	b.Run("diff", func(b *testing.B) {
		query := domain.ChangeProposalDiffInput{ProposalID: proposal.ID, ActorID: author}
		b.ReportAllocs()
		for b.Loop() {
			diff, err := store.GetChangeProposalDiffSources(b.Context(), query)
			if err != nil || diff.BaseRevisionID != item.CurrentRevisionID {
				b.Fatal("unexpected diff basis", err)
			}
		}
	})
}
