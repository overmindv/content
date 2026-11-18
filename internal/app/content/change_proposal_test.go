package content

import (
	"testing"

	"github.com/overmindv/content/internal/mock"
	contentpb "github.com/overmindv/content/internal/pkg/api/content"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCChangeProposal(t *testing.T) {
	store := mock.NewMockChangeProposalStore(gomock.NewController(t))
	ctx, client := newGRPCTestClient(t, testServiceToken, NewServer(nil, service.NewChangeProposalService(store)))
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
