//go:build component

package content

import (
	"testing"

	"github.com/google/uuid"
	contentpb "github.com/overmindv/content/internal/pkg/api/content"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCRevisionPublicationWithPostgres(t *testing.T) {
	ctx, client := startPostgresContentClient(t)
	token := "Bearer " + testServiceToken
	author := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", token, "x-user-id", testActor))
	reader := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", token, "x-user-id", testOtherActor))
	created, err := client.CreateContentItem(author, &contentpb.CreateContentItemRequest{
		Type: "article", Title: "Publication lifecycle", Slug: "publication-" + uuid.NewString(),
		Format: "markdown", Source: "# First version",
	})
	if err != nil {
		t.Fatal(err)
	}
	item := created.GetItem()
	id, first := item.GetId(), item.GetCurrentRevisionId()
	if id == "" || first == "" {
		t.Fatal("expected an item and its first revision")
	}
	publish := &contentpb.PublishContentItemRequest{Id: id, ExpectedRevisionId: first}
	if _, err = client.PublishContentItem(author, publish); err != nil {
		t.Fatal(err)
	}

	saved, err := client.CreateContentRevision(author, &contentpb.CreateContentRevisionRequest{
		ContentItemId: id, ExpectedRevisionId: first, Format: "markdown", Source: "# Second version",
	})
	if err != nil {
		t.Fatal(err)
	}
	second := saved.GetRevision().GetId()
	if second == "" || second == first {
		t.Fatal("expected a new revision ID")
	}
	get := &contentpb.GetContentItemRequest{Id: id}
	expectPublicRevision := func(expectedID, expectedSource string) {
		t.Helper()
		response, readErr := client.GetContentItem(reader, get)
		if readErr != nil {
			t.Fatal(readErr)
		}
		public := response.GetItem()
		if public.GetCurrentRevisionId() != expectedID || public.GetPublishedRevisionId() != expectedID ||
			public.GetCurrentRevision().GetSource() != expectedSource {
			t.Error("reader did not receive the published revision")
		}
	}

	expectPublicRevision(first, "# First version")
	_, err = client.PublishContentItem(author, publish)
	if got := status.Code(err); got != codes.Aborted {
		t.Errorf("publish with a stale revision = %v, want Aborted", got)
	}
	expectPublicRevision(first, "# First version")

	if _, err = client.PublishContentItem(author, &contentpb.PublishContentItemRequest{Id: id, ExpectedRevisionId: second}); err != nil {
		t.Fatal(err)
	}
	expectPublicRevision(second, "# Second version")
	if _, err = client.ArchiveContentItem(author, &contentpb.ArchiveContentItemRequest{Id: id, ExpectedRevisionId: second}); err != nil {
		t.Fatal(err)
	}
	_, err = client.GetContentItem(reader, get)
	if got := status.Code(err); got != codes.NotFound {
		t.Errorf("read archived material = %v, want NotFound", got)
	}
	_, err = client.CreateContentRevision(author, &contentpb.CreateContentRevisionRequest{
		ContentItemId: id, ExpectedRevisionId: second, Format: "markdown", Source: "# Archived edit",
	})
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("edit archived material = %v, want FailedPrecondition", got)
	}
}
