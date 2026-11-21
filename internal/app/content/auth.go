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

func requireServiceActor(token string) grpc.UnaryServerInterceptor {
	configured := len(token) >= 32
	bearer := "Bearer " + token
	digest := sha256.Sum256([]byte(bearer))

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		auth := metadata.ValueFromIncomingContext(ctx, "authorization")
		if !configured || len(auth) != 1 || len(auth[0]) != len(bearer) {
			return nil, status.Error(codes.Unauthenticated, "service authentication required")
		}
		presented := sha256.Sum256([]byte(auth[0]))
		if subtle.ConstantTimeCompare(digest[:], presented[:]) != 1 {
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
