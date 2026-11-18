package content

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/overmindv/content/internal/mock"
	contentpb "github.com/overmindv/content/internal/pkg/api/content"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const (
	testServiceToken = "test-only-token"
	testActor        = "11111111-1111-4111-8111-111111111111"
	testOtherActor   = "22222222-2222-4222-8222-222222222222"
)

func TestGRPCRequiresServiceActor(t *testing.T) {
	bearer := "Bearer " + testServiceToken
	for _, tt := range []struct {
		name   string
		fields []string
	}{
		{"missing token", []string{"x-user-id", testActor}},
		{"incorrect token", []string{"authorization", "Bearer test-only-fake!", "x-user-id", testActor}},
		{"duplicate token", []string{"authorization", bearer, "authorization", bearer, "x-user-id", testActor}},
		{"missing actor", []string{"authorization", bearer}},
		{"malformed actor", []string{"authorization", bearer, "x-user-id", "not-a-uuid"}},
		{"nil actor", []string{"authorization", bearer, "x-user-id", "00000000-0000-0000-0000-000000000000"}},
		{"duplicate actor", []string{"authorization", bearer, "x-user-id", testActor, "x-user-id", testOtherActor}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, client, _ := newAuthTestClient(t, testServiceToken)
			ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(tt.fields...))
			_, err := client.GetContentItem(ctx, &contentpb.GetContentItemRequest{Id: testActor})
			if got := status.Code(err); got != codes.Unauthenticated {
				t.Errorf("GetContentItem status = %v, want Unauthenticated", got)
			}
		})
	}
	t.Run("no configured token", func(t *testing.T) {
		ctx, client, _ := newAuthTestClient(t, "")
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer ", "x-user-id", testActor))
		_, err := client.ListContentItems(ctx, &contentpb.ListContentItemsRequest{})
		if got := status.Code(err); got != codes.Unauthenticated {
			t.Errorf("ListContentItems status = %v, want Unauthenticated", got)
		}
	})
}

func TestGRPCUsesActorForAuthorFields(t *testing.T) {
	ctx, client, store := newAuthTestClient(t, testServiceToken)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	store.EXPECT().Create(gomock.Any(), gomock.Cond(func(input domain.CreateContentItemInput) bool {
		return input.CreatedBy == testActor
	})).Return(domain.ContentItem{CreatedBy: testActor}, nil)
	store.EXPECT().Update(gomock.Any(), gomock.Cond(func(input domain.UpdateContentItemInput) bool {
		return input.UpdatedBy == testActor
	})).Return(domain.ContentItem{UpdatedBy: testActor}, nil)
	store.EXPECT().CreateRevision(gomock.Any(), gomock.Cond(func(input domain.CreateContentRevisionInput) bool {
		return input.CreatedBy == testActor
	})).Return(domain.ContentRevision{CreatedBy: testActor}, nil)

	created, err := client.CreateContentItem(ctx, &contentpb.CreateContentItemRequest{
		Type: "article", Title: "Go", Slug: "go", Source: "# Go", CreatedBy: testOtherActor,
	})
	if err != nil || created.GetItem().GetCreatedBy() != testActor {
		t.Errorf("CreateContentItem did not use the verified actor: %v", err)
	}
	updated, err := client.UpdateContentItem(ctx, &contentpb.UpdateContentItemRequest{
		Id: testActor, Type: "article", Status: "draft", Title: "Go", Slug: "go", UpdatedBy: testOtherActor,
	})
	if err != nil || updated.GetItem().GetUpdatedBy() != testActor {
		t.Errorf("UpdateContentItem did not use the verified actor: %v", err)
	}
	revision, err := client.CreateContentRevision(ctx, &contentpb.CreateContentRevisionRequest{
		ContentItemId: testActor, Source: "# Go", CreatedBy: testOtherActor,
	})
	if err != nil || revision.GetRevision().GetCreatedBy() != testActor {
		t.Errorf("CreateContentRevision did not use the verified actor: %v", err)
	}
}

func TestGRPCPassesVerifiedActorToStore(t *testing.T) {
	ctx, client, store := newAuthTestClient(t, testServiceToken)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	store.EXPECT().Get(gomock.Any(), testOtherActor, testActor).Return(domain.ContentItem{}, service.ErrNotFound)
	store.EXPECT().List(gomock.Any(), testActor).Return(nil, nil)
	store.EXPECT().ListRevisions(gomock.Any(), testOtherActor, testActor).Return(nil, service.ErrNotFound)
	store.EXPECT().Delete(gomock.Any(), testOtherActor, testActor).Return(domain.ContentItem{}, service.ErrNotFound)

	_, err := client.GetContentItem(ctx, &contentpb.GetContentItemRequest{Id: testOtherActor})
	if got := status.Code(err); got != codes.NotFound {
		t.Errorf("GetContentItem status = %v, want NotFound", got)
	}
	if _, err = client.ListContentItems(ctx, &contentpb.ListContentItemsRequest{}); err != nil {
		t.Errorf("ListContentItems: %v", err)
	}
	_, err = client.ListContentRevisions(ctx, &contentpb.ListContentRevisionsRequest{ContentItemId: testOtherActor})
	if got := status.Code(err); got != codes.NotFound {
		t.Errorf("ListContentRevisions status = %v, want NotFound", got)
	}
	_, err = client.DeleteContentItem(ctx, &contentpb.DeleteContentItemRequest{Id: testOtherActor})
	if got := status.Code(err); got != codes.NotFound {
		t.Errorf("DeleteContentItem status = %v, want NotFound", got)
	}
}

func BenchmarkServiceActor(b *testing.B) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer "+testServiceToken, "x-user-id", testActor,
	))
	next := func(context.Context, any) (any, error) { return nil, nil }
	verify := requireServiceActor(testServiceToken)
	b.Run("direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = next(ctx, nil)
		}
	})
	b.Run("checked", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := verify(ctx, nil, nil, next); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func newAuthTestClient(t *testing.T, token string) (context.Context, contentpb.ContentServiceClient, *mock.MockContentItemStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	store := mock.NewMockContentItemStore(gomock.NewController(t))
	listener := bufconn.Listen(64 * 1024)
	server := newGRPCServer(service.NewContentItemService(store), token)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///content", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return ctx, contentpb.NewContentServiceClient(conn), store
}
