package content

import (
	"testing"

	"github.com/overmindv/content/internal/mock"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/pkg/api/activity"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCMaterialActivity(t *testing.T) {
	store := mock.NewMockContentItemStore(gomock.NewController(t))
	ctx, client := newGRPCTestClient(t, testServiceToken, NewServer(service.NewContentItemService(store, nil), nil, nil))
	if _, err := client.SnapshotMaterials(ctx, &contentpb.SnapshotMaterialsRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("snapshot without service token: %v", err)
	}
	machine := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken))
	if _, err := client.SnapshotMaterials(machine, &contentpb.SnapshotMaterialsRequest{AfterId: testActor}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("cursor without watermark: %v", err)
	}
	initial := int64(0)
	store.EXPECT().SnapshotMaterials(gomock.Any(), domain.MaterialSnapshotInput{Watermark: &initial, PageSize: 50}).
		Return(domain.MaterialSnapshot{Watermark: initial, Entries: []domain.MaterialVersion{{
			MaterialID: testActor, RevisionID: testOtherActor,
			State: activity.MaterialState{Metadata: &activity.ResourceMetadata{Title: "Published article", Format: "markdown"}},
		}}}, nil)
	got, err := client.SnapshotMaterials(machine, &contentpb.SnapshotMaterialsRequest{Watermark: &initial})
	if err != nil || len(got.GetEntries()) != 1 || got.GetEntries()[0].GetMetadata().GetFormat() != "markdown" {
		t.Errorf("snapshot at version zero: %+v %v", got, err)
	}
	read := &contentpb.ConfirmMaterialReadRequest{MaterialId: testActor, RevisionId: testOtherActor}
	if _, err = client.ConfirmMaterialRead(machine, read); status.Code(err) != codes.Unauthenticated {
		t.Errorf("confirmation without verified actor: %v", err)
	}
	user := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testOtherActor))
	store.EXPECT().ConfirmMaterialRead(gomock.Any(), domain.ConfirmMaterialReadInput{
		MaterialID: testActor, RevisionID: testOtherActor, ActorID: testOtherActor,
	}).Return(nil)
	if _, err = client.ConfirmMaterialRead(user, read); err != nil {
		t.Errorf("confirmation: %v", err)
	}
}
