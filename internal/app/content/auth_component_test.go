//go:build component

package content

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	contentpb "github.com/overmindv/content/internal/pkg/api/content"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func startPostgresContentClient(t *testing.T) (context.Context, contentpb.ContentServiceClient) {
	t.Helper()
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN is required")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		t.Fatal("COMPONENT_TEST_DSN must point to a dedicated database ending in _test")
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal("could not open test database")
	}
	t.Cleanup(pool.Close)

	store := postgres.NewContentItemStore(pool)
	handler := NewServer(service.NewContentItemService(store), service.NewChangeProposalService(store))

	return newGRPCTestClient(t, testServiceToken, handler)
}

func TestGRPCDraftOwnershipWithPostgres(t *testing.T) {
	ctx, client := startPostgresContentClient(t)
	token := "Bearer " + testServiceToken
	author := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", token, "x-user-id", testActor))
	other := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"authorization", token, "x-user-id", testOtherActor, "x-user-role", "admin",
	))
	slug := "draft-" + uuid.NewString()
	created, err := client.CreateContentItem(author, &contentpb.CreateContentItemRequest{
		Type: "article", Status: "draft", Title: "Original draft", Slug: slug,
		Format: "markdown", Source: "# Private draft", CreatedBy: testOtherActor,
	})
	if err != nil {
		t.Fatal(err)
	}
	item := created.GetItem()
	if item.GetCreatedBy() != testActor || item.GetId() == "" {
		t.Fatal("draft must belong to the verified actor")
	}
	get := &contentpb.GetContentItemRequest{Id: item.GetId()}

	_, err = client.GetContentItem(other, get)
	if got := status.Code(err); got != codes.NotFound {
		t.Errorf("GetContentItem as another user = %v, want NotFound", got)
	}
	_, err = client.UpdateContentItem(other, &contentpb.UpdateContentItemRequest{
		Id: item.GetId(), Type: "article", Status: "draft", Title: "Spoofed draft", Slug: slug,
		UpdatedBy: testActor,
	})
	if got := status.Code(err); got != codes.NotFound {
		t.Errorf("UpdateContentItem as another user = %v, want NotFound", got)
	}
	own, err := client.GetContentItem(author, get)
	if err != nil || own.GetItem().GetTitle() != "Original draft" {
		t.Errorf("author could not read the unchanged draft: %v", err)
	}
}
