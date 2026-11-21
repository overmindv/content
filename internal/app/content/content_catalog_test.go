package content

import (
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

func TestGRPCContentCatalogAndTopicBinding(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mock.NewMockContentItemStore(ctrl)
	topics := mock.NewMockTopicChecker(ctrl)
	handler := NewServer(service.NewContentItemService(store, topics), nil, nil)
	ctx, client := newGRPCTestClient(t, testServiceToken, handler)
	if _, err := client.ListContentCatalog(ctx, &contentpb.ListContentCatalogRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("catalog without credentials: %v", err)
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+testServiceToken, "x-user-id", testActor))
	if _, err := client.ListContentCatalog(ctx, &contentpb.ListContentCatalogRequest{Cursor: "wrong"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("catalog with invalid cursor: %v", err)
	}
	store.EXPECT().ListCatalog(gomock.Any(), domain.CatalogFilter{ActorID: testActor, TopicID: testOtherActor, PageSize: 20}, nil).
		Return([]domain.CatalogEntry{{ID: testActor, Learning: domain.LearningMetadata{TopicIDs: []string{testOtherActor}}}}, nil)
	response, err := client.ListContentCatalog(ctx, &contentpb.ListContentCatalogRequest{TopicId: testOtherActor})
	if err != nil || len(response.GetEntries()) != 1 || len(response.GetEntries()[0].GetLearning().GetTopicIds()) != 1 {
		t.Errorf("catalog with topic filter: %v", err)
	}
	topics.EXPECT().CheckActiveTopic(gomock.Any(), testOtherActor).Return(service.ErrInactiveTopic)
	request := &contentpb.CreateContentItemRequest{
		Type: "article", Title: "Go", Slug: "go", Source: "# Go",
		Learning: &contentpb.ContentLearningMetadata{TopicIds: []string{testOtherActor}},
	}
	if _, err = client.CreateContentItem(ctx, request); status.Code(err) != codes.InvalidArgument {
		t.Errorf("create with inactive topic: %v", err)
	}
	topics.EXPECT().CheckActiveTopic(gomock.Any(), testOtherActor).Return(service.ErrTopicCatalogUnavailable)
	if _, err = client.CreateContentItem(ctx, request); status.Code(err) != codes.Unavailable {
		t.Errorf("create when entities is unavailable: %v", err)
	}
}
