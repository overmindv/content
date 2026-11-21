package content

import (
	"context"
	"strings"
	"testing"

	"github.com/overmindv/content/internal/config"
	"github.com/overmindv/content/internal/mock"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCCatalogCredentialScope(t *testing.T) {
	t.Parallel()

	token := strings.Repeat("a", 32)
	store := mock.NewMockContentItemStore(gomock.NewController(t))
	handler := NewServer(service.NewContentItemService(store, nil), nil, nil)
	cfg := config.GRPCConfig{ServiceToken: testServiceToken, ModerationToken: moderationFixture, CatalogToken: token}
	ctx, conn := newGRPCTestConn(t, cfg, handler)
	client := contentpb.NewContentServiceClient(conn)
	catalog := contentpb.NewMaterialCatalogServiceClient(conn)
	send := func(token string, values ...string) context.Context {
		return metadata.NewOutgoingContext(ctx, metadata.Pairs(append([]string{"authorization", "Bearer " + token}, values...)...))
	}
	machine := send(token)
	withActor := send(token, "x-user-id", testActor)
	key := &contentpb.MaterialVersionKey{MaterialId: testActor, RevisionId: testOtherActor}
	two := []*contentpb.MaterialVersionKey{key, {MaterialId: testOtherActor, RevisionId: testActor}}
	req := &contentpb.CheckMaterialAvailabilityRequest{Resources: two}

	store.EXPECT().SnapshotMaterials(gomock.Any(), domain.MaterialSnapshotInput{PageSize: 50}).
		Return(domain.MaterialSnapshot{}, nil)
	if _, err := client.SnapshotMaterials(machine, &contentpb.SnapshotMaterialsRequest{}); err != nil {
		t.Fatal("catalog credential cannot read snapshot:", err)
	}
	only := domain.MaterialVersionKey{MaterialID: testActor, RevisionID: testOtherActor}
	store.EXPECT().AvailableMaterialVersions(gomock.Any(), []domain.MaterialVersionKey{only, {MaterialID: testOtherActor, RevisionID: testActor}}).
		Return([]domain.MaterialVersionKey{only}, nil)
	reply, checkErr := catalog.CheckMaterialAvailability(machine, req)
	if checkErr != nil || len(reply.GetResources()) != 1 || reply.GetResources()[0].GetMaterialId() != testActor ||
		reply.GetResources()[0].GetRevisionId() != testOtherActor {
		t.Fatal("batch check returned wrong keys:", reply, checkErr)
	}

	for _, credential := range []string{testServiceToken, moderationFixture, ""} {
		_, err := catalog.CheckMaterialAvailability(send(credential, "x-user-id", testActor), req)
		if status.Code(err) != codes.Unauthenticated {
			t.Error("non-catalog credential accepted:", err)
		}
	}
	if _, err := client.GetPublishedContentItem(withActor, &contentpb.GetPublishedContentItemRequest{Id: testActor}); status.Code(err) != codes.Unauthenticated {
		t.Error("catalog credential can read published source:", err)
	}
	if _, err := client.ArchiveContentItem(withActor, &contentpb.ArchiveContentItemRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Error("catalog credential can mutate publications:", err)
	}
	if _, err := client.SetMaterialModeration(withActor, &contentpb.SetMaterialModerationRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Error("catalog credential can moderate:", err)
	}
	if _, err := client.ConfirmMaterialRead(withActor, &contentpb.ConfirmMaterialReadRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Error("catalog credential can claim reading:", err)
	}
	_, err := catalog.CheckMaterialAvailability(send(token, "authorization", "Bearer "+token), req)
	if status.Code(err) != codes.Unauthenticated {
		t.Error("duplicate authorization accepted:", err)
	}
}

func TestGRPCRejectsInvalidAvailabilityBatch(t *testing.T) {
	t.Parallel()

	store := mock.NewMockContentItemStore(gomock.NewController(t))
	token := strings.Repeat("a", 32)
	handler := NewServer(service.NewContentItemService(store, nil), nil, nil)
	ctx, conn := newGRPCTestConn(t, config.GRPCConfig{CatalogToken: token}, handler)
	client := contentpb.NewMaterialCatalogServiceClient(conn)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
	key := &contentpb.MaterialVersionKey{MaterialId: testActor, RevisionId: testOtherActor}
	for _, resources := range [][]*contentpb.MaterialVersionKey{
		nil, {nil}, {key, key}, {{MaterialId: testActor, RevisionId: "invalid"}},
		make([]*contentpb.MaterialVersionKey, domain.MaxMaterialAvailabilityBatch+1),
	} {
		req := &contentpb.CheckMaterialAvailabilityRequest{Resources: resources}
		if _, err := client.CheckMaterialAvailability(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Error("invalid batch accepted:", err)
		}
	}
}

func TestCatalogTokenMustNotMatchOtherCredentials(t *testing.T) {
	t.Parallel()

	token := strings.Repeat("a", 32)
	for _, cfg := range []config.Config{
		{GRPC: config.GRPCConfig{CatalogToken: token, ServiceToken: token}},
		{GRPC: config.GRPCConfig{CatalogToken: token, ModerationToken: token}},
		{GRPC: config.GRPCConfig{CatalogToken: token}, MediaToken: token},
		{GRPC: config.GRPCConfig{CatalogToken: token}, ReviewCandidates: config.ReviewCandidatesConfig{Token: token}},
		{GRPC: config.GRPCConfig{CatalogToken: token + " "}},
	} {
		if err := validateCatalogConfig(cfg); err == nil {
			t.Error("invalid catalog credential accepted")
		}
	}
	if err := validateCatalogConfig(config.Config{GRPC: config.GRPCConfig{CatalogToken: token}}); err != nil {
		t.Fatal(err)
	}
}
