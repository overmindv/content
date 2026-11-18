package content

import (
	"strings"
	"testing"

	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestRestorePublishedRevisionUsesVerifiedActor(t *testing.T) {
	ctx, client, store := newAuthTestClient(t, testServiceToken)
	req := &contentpb.RestorePublishedRevisionRequest{
		ContentItemId: testOtherActor, ExpectedPublishedRevisionId: testActor,
		SourceRevisionId: "  " + strings.ToUpper("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa") + "  ",
	}
	if _, err := client.RestorePublishedRevision(ctx, req); status.Code(err) != codes.Unauthenticated {
		t.Errorf("unauthenticated restore: %v", err)
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	if _, err := client.RestorePublishedRevision(ctx, &contentpb.RestorePublishedRevisionRequest{
		ContentItemId: "invalid", ExpectedPublishedRevisionId: testActor, SourceRevisionId: testOtherActor,
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("invalid material ID: %v", err)
	}
	source := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	store.EXPECT().RestorePublishedRevision(gomock.Any(), domain.RestorePublishedRevisionInput{
		ContentItemID: testOtherActor, ExpectedPublishedID: testActor, SourceRevisionID: source, ActorID: testActor,
	}).Return(domain.ContentRevision{RestoredFromRevisionID: source, CreatedBy: testActor}, nil)
	got, err := client.RestorePublishedRevision(ctx, req)
	if err != nil || got.GetRevision().GetRestoredFromRevisionId() != source || got.GetRevision().GetCreatedBy() != testActor {
		t.Errorf("restore response: %v", err)
	}
}
