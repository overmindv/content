package content

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"

	"github.com/google/uuid"
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

func requireServiceActor(token, moderationToken string) grpc.UnaryServerInterceptor {
	service, moderation := credentialFor(token), credentialFor(moderationToken)

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		credential := &service
		if info != nil {
			switch info.FullMethod {
			case contentpb.ContentService_GetMaterialModeration_FullMethodName, contentpb.ContentService_SetMaterialModeration_FullMethodName:
				credential = &moderation
			}
		}
		auth := metadata.ValueFromIncomingContext(ctx, "authorization")
		if !credential.configured || len(auth) != 1 || len(auth[0]) != credential.length {
			return nil, status.Error(codes.Unauthenticated, "service authentication required")
		}
		presented := sha256.Sum256([]byte(auth[0]))
		if subtle.ConstantTimeCompare(credential.digest[:], presented[:]) != 1 {
			return nil, status.Error(codes.Unauthenticated, "service authentication required")
		}
		if info != nil && info.FullMethod == contentpb.ContentService_SnapshotMaterials_FullMethodName {
			return next(ctx, req)
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
