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

const moderationFixture = "fixture-for-content-moderation-tests"

func TestModerationServiceCredential(t *testing.T) {
	store := mock.NewMockContentItemStore(gomock.NewController(t))
	handler := NewServer(service.NewContentItemService(store, nil), nil, nil)
	ctx, client := newGRPCTestClientWithModeration(t, testServiceToken, moderationFixture, handler)
	actor := func(token string, extra ...string) context.Context {
		return metadata.NewOutgoingContext(ctx, metadata.Pairs(append([]string{
			"authorization", "Bearer " + token, "x-user-id", testActor,
		}, extra...)...))
	}
	read := &contentpb.GetMaterialModerationRequest{MaterialId: testOtherActor}
	version := int64(0)
	set := &contentpb.SetMaterialModerationRequest{
		MaterialId: testOtherActor, ExpectedPublishedRevisionId: testActor, ExpectedModerationVersion: &version,
		Action: contentpb.MaterialModerationAction_MATERIAL_MODERATION_ACTION_HIDE, Reason: "  Incorrect content  ",
	}
	spoofed := actor(testServiceToken, "x-role", "admin", "x-is-admin", "true")
	if _, err := client.GetMaterialModeration(spoofed, read); status.Code(err) != codes.Unauthenticated {
		t.Errorf("normal service credential could read moderation state: %v", err)
	}
	if _, err := client.SetMaterialModeration(spoofed, set); status.Code(err) != codes.Unauthenticated {
		t.Errorf("normal service credential could moderate: %v", err)
	}
	if _, err := client.GetContentItem(actor(moderationFixture), &contentpb.GetContentItemRequest{Id: testOtherActor}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("moderation credential could call ordinary methods: %v", err)
	}
	noActor := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+moderationFixture))
	if _, err := client.SetMaterialModeration(noActor, set); status.Code(err) != codes.Unauthenticated {
		t.Errorf("moderation without actor: %v", err)
	}
	disabledCtx, disabled := newGRPCTestClient(t, testServiceToken, handler)
	disabledCtx = metadata.NewOutgoingContext(disabledCtx, metadata.Pairs("authorization", "Bearer ", "x-user-id", testActor))
	if _, err := disabled.GetMaterialModeration(disabledCtx, read); status.Code(err) != codes.Unauthenticated {
		t.Errorf("disabled moderation accepted empty credential: %v", err)
	}

	verified := actor(moderationFixture)
	store.EXPECT().GetMaterialModeration(gomock.Any(), testOtherActor).
		Return(domain.MaterialModeration{MaterialID: testOtherActor, PublishedRevisionID: testActor}, nil)
	if value, err := client.GetMaterialModeration(verified, read); err != nil || value.GetMaterialId() != testOtherActor {
		t.Errorf("authorized state lookup: %v", err)
	}
	store.EXPECT().SetMaterialModeration(gomock.Any(), domain.SetMaterialModerationInput{
		ActorID: testActor, Reason: "Incorrect content",
		MaterialModeration: domain.MaterialModeration{MaterialID: testOtherActor, PublishedRevisionID: testActor, Hidden: true},
	}).Return(domain.MaterialModeration{MaterialID: testOtherActor, PublishedRevisionID: testActor, Hidden: true, Version: 1}, nil)
	if value, err := client.SetMaterialModeration(verified, set); err != nil || !value.GetHidden() || value.GetVersion() != 1 {
		t.Errorf("authorized moderation: %v", err)
	}

	set.ExpectedModerationVersion = nil
	if _, err := client.SetMaterialModeration(verified, set); status.Code(err) != codes.InvalidArgument {
		t.Errorf("missing version accepted: %v", err)
	}
	set.ExpectedModerationVersion = &version
	for _, reason := range []string{"  ", "a\nb", strings.Repeat("x", 1025)} {
		set.Reason = reason
		if _, err := client.SetMaterialModeration(verified, set); status.Code(err) != codes.InvalidArgument {
			t.Errorf("invalid reason accepted: %v", err)
		}
	}
	set.Reason, set.Action = "reason", contentpb.MaterialModerationAction_MATERIAL_MODERATION_ACTION_UNSPECIFIED
	if _, err := client.SetMaterialModeration(verified, set); status.Code(err) != codes.InvalidArgument {
		t.Errorf("missing action accepted: %v", err)
	}
}

func TestModerationConfig(t *testing.T) {
	cfg := config.Config{GRPC: config.GRPCConfig{ServiceToken: moderationFixture}}
	if err := validateModerationConfig(cfg); err != nil {
		t.Error(err)
	}
	cfg.GRPC.ModerationToken = moderationFixture
	if err := validateModerationConfig(cfg); err == nil {
		t.Error("reused token accepted")
	}
	cfg.GRPC.ServiceToken = testServiceToken
	if err := validateModerationConfig(cfg); err == nil {
		t.Error("missing Kafka activity accepted")
	}
	cfg.Kafka.Enabled, cfg.Kafka.ActivityEnabled = true, true
	if err := validateModerationConfig(cfg); err != nil {
		t.Error(err)
	}
}
