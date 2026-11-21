package content

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/content/internal/mock"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/validator"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const (
	testServiceToken = "test-only-content-token-00000000000000"
	testActor        = "11111111-1111-4111-8111-111111111111"
	testOtherActor   = "22222222-2222-4222-8222-222222222222"
)

func TestGRPCRequiresServiceActor(t *testing.T) {
	t.Parallel()

	bearer := "Bearer " + testServiceToken
	wrongBearer := bearer[:len(bearer)-1] + "X"
	for _, tt := range []struct {
		name   string
		fields []string
	}{
		{"missing token", []string{"x-user-id", testActor}},
		{"incorrect token", []string{"authorization", "Bearer test-only-fake!", "x-user-id", testActor}},
		{"incorrect same-length token", []string{"authorization", wrongBearer, "x-user-id", testActor}},
		{"duplicate token", []string{"authorization", bearer, "authorization", bearer, "x-user-id", testActor}},
		{"missing actor", []string{"authorization", bearer}},
		{"malformed actor", []string{"authorization", bearer, "x-user-id", "not-a-uuid"}},
		{"nil actor", []string{"authorization", bearer, "x-user-id", "00000000-0000-0000-0000-000000000000"}},
		{"duplicate actor", []string{"authorization", bearer, "x-user-id", testActor, "x-user-id", testOtherActor}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, client, _ := newAuthTestClient(t, testServiceToken)
			ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(tt.fields...))
			_, err := client.GetContentItem(ctx, &contentpb.GetContentItemRequest{Id: testActor})
			if got := status.Code(err); got != codes.Unauthenticated {
				t.Errorf("GetContentItem status = %v, want Unauthenticated", got)
			}
		})
	}
	for _, token := range []string{"", "__GENERATE__"} {
		t.Run("reject unconfigured token "+token, func(t *testing.T) {
			t.Parallel()

			ctx, client, _ := newAuthTestClient(t, token)
			ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token, "x-user-id", testActor))
			_, err := client.ListContentItems(ctx, &contentpb.ListContentItemsRequest{})
			if got := status.Code(err); got != codes.Unauthenticated {
				t.Errorf("ListContentItems status = %v, want Unauthenticated", got)
			}
		})
	}
}

func TestGRPCPublishedReadRequiresActorButDoesNotSelectDraft(t *testing.T) {
	t.Parallel()

	store := mock.NewMockContentItemStore(gomock.NewController(t))
	handler := NewServer(service.NewContentItemService(store, nil), nil, nil)
	ctx, client := newGRPCTestClient(t, testServiceToken, handler)
	req := &contentpb.GetPublishedContentItemRequest{Id: testOtherActor}
	if _, err := client.GetPublishedContentItem(ctx, req); status.Code(err) != codes.Unauthenticated {
		t.Errorf("published read without actor: %v", err)
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	store.EXPECT().Get(gomock.Any(), testOtherActor, "").Return(domain.ContentItem{ID: testOtherActor}, nil)
	result, err := client.GetPublishedContentItem(ctx, req)
	if err != nil || result.GetItem().GetId() != testOtherActor {
		t.Errorf("published response: %v", err)
	}
}

func TestGRPCUsesActorForAuthorFields(t *testing.T) {
	t.Parallel()

	ctx, client, store := newAuthTestClient(t, testServiceToken)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	store.EXPECT().Create(gomock.Any(), gomock.Cond(func(input domain.CreateContentItemInput) bool {
		return input.CreatedBy == testActor
	})).Return(domain.ContentItem{CreatedBy: testActor}, nil)
	store.EXPECT().Update(gomock.Any(), gomock.Cond(func(input domain.UpdateContentItemInput) bool {
		return input.UpdatedBy == testActor
	})).Return(domain.ContentItem{UpdatedBy: testActor}, nil)
	store.EXPECT().CreateRevision(gomock.Any(), gomock.Cond(func(input domain.CreateContentRevisionInput) bool {
		return input.CreatedBy == testActor && input.ExpectedRevisionID == testActor
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
		ContentItemId: testActor, Source: "# Go", CreatedBy: testOtherActor, ExpectedRevisionId: testActor,
	})
	if err != nil || revision.GetRevision().GetCreatedBy() != testActor {
		t.Errorf("CreateContentRevision did not use the verified actor: %v", err)
	}
}

func TestGRPCSaveDraftUsesVerifiedActorAndExplicitAttachments(t *testing.T) {
	t.Parallel()

	ctx, client, store := newAuthTestClient(t, testServiceToken)
	request := &contentpb.SaveContentDraftRequest{
		Revision: &contentpb.CreateContentRevisionRequest{
			ContentItemId: testActor, Source: "    Go code\n", CreatedBy: testOtherActor,
			ExpectedRevisionId: testActor, Attachments: &contentpb.AttachmentSet{},
		},
		DraftMetadata: &contentpb.DraftMetadata{Title: " Go basics "},
	}
	if _, err := client.SaveContentDraft(ctx, request); status.Code(err) != codes.Unauthenticated {
		t.Error("save without a verified actor", err)
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	store.EXPECT().CreateRevision(gomock.Any(), gomock.Cond(func(input domain.CreateContentRevisionInput) bool {
		return input.CreatedBy == testActor && input.Source == request.GetRevision().GetSource() &&
			input.ExpectedRevisionID == testActor && input.DraftMetadata != nil && input.DraftMetadata.Title == "Go basics" &&
			input.Attachments != nil && len(input.Attachments.Assets) == 0
	})).Return(domain.ContentRevision{CreatedBy: testActor}, nil)
	saved, err := client.SaveContentDraft(ctx, request)
	if err != nil || saved.GetRevision().GetCreatedBy() != testActor {
		t.Error("save did not use the verified actor", err)
	}
	request.DraftMetadata = nil
	if _, err = client.SaveContentDraft(ctx, request); status.Code(err) != codes.InvalidArgument {
		t.Error("save without draft metadata", err)
	}
}

func TestGRPCRevisionState(t *testing.T) {
	t.Parallel()

	ctx, client, store := newAuthTestClient(t, testServiceToken)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	state := domain.ChangeContentItemStateInput{ID: testOtherActor, ActorID: testActor, ExpectedRevisionID: testActor}
	store.EXPECT().ChangeState(gomock.Any(), state, domain.ContentStatusPublished).
		Return(domain.ContentItem{}, service.ErrRevisionConflict)
	store.EXPECT().ChangeState(gomock.Any(), state, domain.ContentStatusArchived).
		Return(domain.ContentItem{Status: domain.ContentStatusArchived}, nil)

	_, err := client.CreateContentItem(ctx, &contentpb.CreateContentItemRequest{
		Type: "article", Status: "published", Title: "Go", Slug: "go", Source: "# Go",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("CreateContentItem with published status: %v", err)
	}
	_, err = client.UpdateContentItem(ctx, &contentpb.UpdateContentItemRequest{
		Id: testOtherActor, Type: "article", Status: "published", Title: "Go", Slug: "go",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("UpdateContentItem with published status: %v", err)
	}
	_, err = client.CreateContentRevision(ctx, &contentpb.CreateContentRevisionRequest{ContentItemId: testOtherActor, Source: "# Go"})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("CreateContentRevision without expected revision: %v", err)
	}
	_, err = client.PublishContentItem(ctx, &contentpb.PublishContentItemRequest{Id: testOtherActor, ExpectedRevisionId: testActor})
	if status.Code(err) != codes.Aborted {
		t.Errorf("PublishContentItem with stale revision: %v", err)
	}
	archived, err := client.ArchiveContentItem(ctx, &contentpb.ArchiveContentItemRequest{Id: testOtherActor, ExpectedRevisionId: testActor})
	if err != nil || archived.GetItem().GetStatus() != "archived" {
		t.Errorf("ArchiveContentItem with current revision: %v", err)
	}
}

func TestGRPCRejectsUnsupportedSource(t *testing.T) {
	t.Parallel()

	ctx, client, _ := newAuthTestClient(t, testServiceToken)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	for _, tt := range []struct {
		source string
		code   codes.Code
	}{
		{"<p>Example</p>", codes.InvalidArgument},
		{strings.Repeat(" ", validator.MaxSourceBytes) + "# Go", codes.InvalidArgument},
		{strings.Repeat("x", 512*1024), codes.ResourceExhausted},
	} {
		_, err := client.CreateContentItem(ctx, &contentpb.CreateContentItemRequest{
			Type: "article", Title: "Go", Slug: "go", Source: tt.source,
		})
		if got := status.Code(err); got != tt.code {
			t.Errorf("CreateContentItem status = %v, want %v", got, tt.code)
		}
	}
}

func TestGRPCPassesVerifiedActorToStore(t *testing.T) {
	t.Parallel()

	ctx, client, store := newAuthTestClient(t, testServiceToken)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"authorization", "Bearer "+testServiceToken, "x-user-id", testActor, "x-user-role", "admin",
	))
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
	store := mock.NewMockContentItemStore(gomock.NewController(t))
	handler := NewServer(service.NewContentItemService(store, nil), nil, nil)
	ctx, client := newGRPCTestClient(t, token, handler)

	return ctx, client, store
}

func newGRPCTestClient(t *testing.T, token string, handler *Server) (context.Context, contentpb.ContentServiceClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	listener := bufconn.Listen(64 * 1024)
	server := newGRPCServer(handler, token)
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

	return ctx, contentpb.NewContentServiceClient(conn)
}
