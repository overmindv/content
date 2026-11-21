package content

import (
	"strings"
	"testing"

	"github.com/overmindv/content/internal/mock"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCChangeProposal(t *testing.T) {
	t.Parallel()

	store := mock.NewMockChangeProposalStore(gomock.NewController(t))
	ctx, client := newGRPCTestClient(t, testServiceToken, NewServer(nil, service.NewChangeProposalService(store), nil))
	req := &contentpb.OpenChangeProposalRequest{ContentItemId: testOtherActor, BaseRevisionId: testOtherActor, Source: "# Proposal"}
	if _, err := client.OpenChangeProposal(ctx, req); status.Code(err) != codes.Unauthenticated {
		t.Errorf("OpenChangeProposal without service credentials: %v", err)
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	input := domain.OpenChangeProposalInput{ContentItemID: testOtherActor, BaseRevisionID: testOtherActor, AuthorID: testActor, Source: req.Source}
	proposal := domain.ChangeProposal{ID: testActor, AuthorID: testActor, Status: domain.ChangeProposalOpen, Head: domain.ChangeProposalRevision{Ordinal: 1}}
	store.EXPECT().OpenChangeProposal(gomock.Any(), input).Return(proposal, nil)
	store.EXPECT().OpenChangeProposal(gomock.Any(), input).Return(domain.ChangeProposal{}, service.ErrProposalExists)
	store.EXPECT().GetChangeProposal(gomock.Any(), testOtherActor, testActor).Return(domain.ChangeProposal{}, service.ErrProposalNotFound)
	store.EXPECT().CloseChangeProposal(gomock.Any(), testOtherActor, testActor).Return(domain.ChangeProposal{}, service.ErrProposalNotFound)
	store.EXPECT().AppendChangeProposalRevision(gomock.Any(), domain.AppendChangeProposalRevisionInput{
		ProposalID: testOtherActor, ExpectedHeadID: testActor, AuthorID: testActor, Source: "# Next",
	}).Return(domain.ChangeProposal{}, service.ErrRevisionConflict)

	resp, err := client.OpenChangeProposal(ctx, req)
	if err != nil || resp.GetProposal().GetAuthorId() != testActor || resp.GetProposal().GetHead().GetOrdinal() != 1 {
		t.Errorf("OpenChangeProposal returned unexpected proposal: %v", err)
	}
	if _, err = client.OpenChangeProposal(ctx, req); status.Code(err) != codes.AlreadyExists {
		t.Errorf("duplicate OpenChangeProposal: %v", err)
	}
	if _, err = client.GetChangeProposal(ctx, &contentpb.GetChangeProposalRequest{ProposalId: testOtherActor}); status.Code(err) != codes.NotFound {
		t.Errorf("GetChangeProposal on a hidden proposal: %v", err)
	}
	if _, err = client.CloseChangeProposal(ctx, &contentpb.CloseChangeProposalRequest{ProposalId: testOtherActor}); status.Code(err) != codes.NotFound {
		t.Errorf("CloseChangeProposal as a non-author: %v", err)
	}
	if _, err = client.AppendChangeProposalRevision(ctx, &contentpb.AppendChangeProposalRevisionRequest{
		ProposalId: testOtherActor, ExpectedHeadId: testActor, Source: "# Next",
	}); status.Code(err) != codes.Aborted {
		t.Errorf("AppendChangeProposalRevision with stale head: %v", err)
	}
	if _, err = client.OpenChangeProposal(ctx, &contentpb.OpenChangeProposalRequest{
		ContentItemId: testOtherActor, BaseRevisionId: testOtherActor, Source: "<p>Example</p>",
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("OpenChangeProposal with raw HTML: %v", err)
	}
}

func TestGRPCChangeProposalHistoryAndDiff(t *testing.T) {
	t.Parallel()

	store := mock.NewMockChangeProposalStore(gomock.NewController(t))
	ctx, client := newGRPCTestClient(t, testServiceToken, NewServer(nil, service.NewChangeProposalService(store), nil))
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	store.EXPECT().ListChangeProposalHistory(gomock.Any(), domain.ChangeProposalHistoryInput{
		ProposalID: testOtherActor, ActorID: testActor, PageSize: 20,
	}).Return(domain.ChangeProposalHistory{Entries: []domain.ChangeProposalHistoryEntry{{ID: testActor, Ordinal: 3}}, NextBeforeOrdinal: 3}, nil)
	store.EXPECT().GetChangeProposalDiffSources(gomock.Any(), domain.ChangeProposalDiffInput{
		ProposalID: testOtherActor, ActorID: testActor,
	}).Return(domain.ChangeProposalDiffSources{BaseRevisionID: testOtherActor, RevisionID: testActor, BaseSource: "old\n", Source: "new\n"}, nil)
	store.EXPECT().GetChangeProposalDiffSources(gomock.Any(), domain.ChangeProposalDiffInput{
		ProposalID: testOtherActor, RevisionID: testOtherActor, ActorID: testActor,
	}).Return(domain.ChangeProposalDiffSources{}, service.ErrProposalRevisionNotFound)

	page, err := client.ListChangeProposalHistory(ctx, &contentpb.ListChangeProposalHistoryRequest{ProposalId: testOtherActor})
	if err != nil || page.GetNextBeforeOrdinal() != 3 || len(page.GetEntries()) != 1 || page.GetEntries()[0].GetOrdinal() != 3 {
		t.Errorf("unexpected history page: %v", err)
	}
	if _, err = client.ListChangeProposalHistory(ctx, &contentpb.ListChangeProposalHistoryRequest{ProposalId: testOtherActor, PageSize: 51}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("history larger than max page size: %v", err)
	}
	diff, err := client.GetChangeProposalDiff(ctx, &contentpb.GetChangeProposalDiffRequest{ProposalId: testOtherActor})
	if err != nil || diff.GetTruncated() || diff.GetRevisionId() != testActor || !strings.Contains(diff.GetUnifiedDiff(), "-old\n+new\n") {
		t.Errorf("unexpected diff: %v", err)
	}
	if _, err = client.GetChangeProposalDiff(ctx, &contentpb.GetChangeProposalDiffRequest{ProposalId: testOtherActor, RevisionId: testOtherActor}); status.Code(err) != codes.NotFound {
		t.Errorf("diff with missing revision: %v", err)
	}
}

func TestGRPCRebaseChangeProposal(t *testing.T) {
	t.Parallel()

	store := mock.NewMockChangeProposalStore(gomock.NewController(t))
	ctx, client := newGRPCTestClient(t, testServiceToken, NewServer(nil, service.NewChangeProposalService(store), nil))
	req := &contentpb.RebaseChangeProposalRequest{
		ProposalId: testOtherActor, ExpectedHeadId: testActor, Source: "# Rebased proposal",
	}
	if _, err := client.RebaseChangeProposal(ctx, req); status.Code(err) != codes.Unauthenticated {
		t.Error("rebase allowed an unauthenticated actor", err)
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	if _, err := client.RebaseChangeProposal(ctx, req); status.Code(err) != codes.InvalidArgument {
		t.Error("rebase accepted a missing base", err)
	}
	req.BaseRevisionId = testOtherActor
	input := domain.RebaseChangeProposalInput{
		AppendChangeProposalRevisionInput: domain.AppendChangeProposalRevisionInput{
			ProposalID: testOtherActor, ExpectedHeadID: testActor, AuthorID: testActor, Source: req.Source,
		},
		BaseRevisionID: testOtherActor,
	}
	store.EXPECT().RebaseChangeProposal(gomock.Any(), input).Return(domain.ChangeProposal{}, service.ErrMergeConflict)
	if _, err := client.RebaseChangeProposal(ctx, req); status.Code(err) != codes.Aborted || status.Convert(err).Message() != "MERGE_CONFLICT" {
		t.Error("rebase did not report a moved publication", err)
	}
}
