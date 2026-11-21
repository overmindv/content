//go:build component

package content

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type attachmentSignatureFixture struct {
	mu       sync.Mutex
	ref      domain.PublishedAttachmentRef
	download domain.AttachmentDownload
	calls    int
}

func (s *attachmentSignatureFixture) SignPublishedAttachment(_ context.Context, ref domain.PublishedAttachmentRef) (domain.AttachmentDownload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.ref = ref

	return s.download, nil
}

func (s *attachmentSignatureFixture) observed() (int, domain.PublishedAttachmentRef) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.calls, s.ref
}

func TestPublishedAttachmentAuthorization(t *testing.T) {
	store := postgresContentFixture(t)
	items := service.NewContentItemService(store, nil)
	signature := &attachmentSignatureFixture{download: domain.AttachmentDownload{
		URL: "https://objects.invalid/media/fixture?signature=fixture", ExpiresAt: time.Now().Add(30 * time.Second),
	}}
	items.SetPublishedAttachmentSigner(signature)
	ctx, client := newGRPCTestClient(t, testServiceToken, NewServer(items, nil, nil))
	auth := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testOtherActor))
	pdfID := uuid.NewString()
	item, err := store.Create(ctx, domain.CreateContentItemInput{
		Type: "article", Status: "draft", Title: "Attachment permissions", Slug: "attachment-" + uuid.NewString(),
		Format: "markdown", Source: "# Published PDF", CreatedBy: testActor,
		Assets: []domain.CreateContentAssetInput{{AssetID: pdfID, Kind: "pdf", Title: "handout.pdf"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first := item.CurrentRevisionID
	check := func(requestContext context.Context, revisionID, fileID string, want codes.Code) {
		t.Helper()
		before, _ := signature.observed()
		link, callErr := client.GetPublishedAttachmentDownload(requestContext, &contentpb.GetPublishedAttachmentDownloadRequest{
			MaterialId: item.ID, RevisionId: revisionID, FileId: fileID,
		})
		if status.Code(callErr) != want {
			t.Error("download status", status.Code(callErr), "want", want)
		}
		calls, ref := signature.observed()
		if want == codes.OK {
			if calls != before+1 || ref != (domain.PublishedAttachmentRef{MaterialID: item.ID, RevisionID: revisionID, FileID: fileID}) ||
				link.GetUrl() != signature.download.URL || !link.GetExpiresAt().AsTime().Equal(signature.download.ExpiresAt) {
				t.Error("wrong attachment or signature")
			}
		} else if calls != before || link != nil {
			t.Error("refused download reached the signer")
		}
	}
	check(ctx, first, pdfID, codes.Unauthenticated)
	check(auth, first, pdfID, codes.NotFound)
	state := domain.ChangeContentItemStateInput{ID: item.ID, ActorID: testActor, ExpectedRevisionID: first}
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusPublished); err != nil {
		t.Fatal(err)
	}
	check(auth, first, pdfID, codes.OK)
	check(auth, first, uuid.NewString(), codes.NotFound)
	check(auth, first, "%2f", codes.InvalidArgument)
	second, err := store.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID: item.ID, ExpectedRevisionID: first, CreatedBy: testActor, Format: "markdown", Source: "# Working revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	check(auth, second.ID, pdfID, codes.NotFound)
	check(auth, first, pdfID, codes.OK)
	state.ExpectedRevisionID = second.ID
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusPublished); err != nil {
		t.Fatal(err)
	}
	check(auth, first, pdfID, codes.NotFound)
	check(auth, second.ID, pdfID, codes.OK)
	if _, err = store.ChangeState(ctx, state, domain.ContentStatusArchived); err != nil {
		t.Fatal(err)
	}
	check(auth, second.ID, pdfID, codes.NotFound)
}
