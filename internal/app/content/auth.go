package content

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/config"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type actorIDKey struct{}

type serviceCredential struct {
	configured bool
	length     int
	digest     [sha256.Size]byte
}

func credentialFor(token string) serviceCredential {
	bearer := "Bearer " + token

	return serviceCredential{configured: len(token) >= 32, length: len(bearer), digest: sha256.Sum256([]byte(bearer))}
}

func (c serviceCredential) matches(auth string) bool {
	if !c.configured || len(auth) != c.length {
		return false
	}
	presented := sha256.Sum256([]byte(auth))

	return subtle.ConstantTimeCompare(c.digest[:], presented[:]) == 1
}

func requireServiceActor(cfg config.GRPCConfig) grpc.UnaryServerInterceptor {
	service := credentialFor(cfg.ServiceToken)
	moderation := credentialFor(cfg.ModerationToken)
	catalog := credentialFor(cfg.CatalogToken)

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		auth := metadata.ValueFromIncomingContext(ctx, "authorization")
		if len(auth) != 1 {
			return nil, status.Error(codes.Unauthenticated, "service authentication required")
		}
		method := ""
		if info != nil {
			method = info.FullMethod
		}
		switch method {
		case contentpb.ContentService_SnapshotMaterials_FullMethodName:
			if catalog.matches(auth[0]) || service.matches(auth[0]) {
				return next(ctx, req)
			}

			return nil, status.Error(codes.Unauthenticated, "service authentication required")
		case contentpb.MaterialCatalogService_CheckMaterialAvailability_FullMethodName:
			if catalog.matches(auth[0]) {
				return next(ctx, req)
			}

			return nil, status.Error(codes.Unauthenticated, "service authentication required")
		}
		credential := service
		if method == contentpb.ContentService_GetMaterialModeration_FullMethodName ||
			method == contentpb.ContentService_SetMaterialModeration_FullMethodName {
			credential = moderation
		}
		if !credential.matches(auth[0]) {
			return nil, status.Error(codes.Unauthenticated, "service authentication required")
		}

		ids := metadata.ValueFromIncomingContext(ctx, "x-user-id")
		if len(ids) != 1 || len(ids[0]) != 36 {
			return nil, status.Error(codes.Unauthenticated, "verified actor required")
		}
		id, err := uuid.Parse(ids[0])
		if err != nil || id == uuid.Nil {
			return nil, status.Error(codes.Unauthenticated, "verified actor required")
		}

		return next(context.WithValue(ctx, actorIDKey{}, id.String()), req)
	}
}

func actorID(ctx context.Context) (string, error) {
	id, ok := ctx.Value(actorIDKey{}).(string)
	if !ok || id == "" {
		return "", status.Error(codes.Unauthenticated, "verified actor required")
	}

	return id, nil
}
