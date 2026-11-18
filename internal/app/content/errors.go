package content

import (
	"context"
	"errors"

	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/validator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func toStatusError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	case errors.Is(err, service.ErrNotFound), errors.Is(err, service.ErrProposalNotFound), errors.Is(err, service.ErrProposalRevisionNotFound),
		errors.Is(err, service.ErrReviewThreadNotFound), errors.Is(err, service.ErrPublishedRevisionNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, service.ErrProposalDiffBusy):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, service.ErrProposalExists), errors.Is(err, service.ErrReviewDecisionExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, service.ErrReviewNotAllowed):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, service.ErrTopicCatalogUnavailable), errors.Is(err, service.ErrMediaUnavailable):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, service.ErrInactiveTopic):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, service.ErrRevisionConflict), errors.Is(err, service.ErrMergeConflict):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, service.ErrInvalidState), errors.Is(err, service.ErrReviewRequired), errors.Is(err, service.ErrAttachmentNotReady), errors.Is(err, service.ErrAttachmentsPending):
		return status.Error(codes.FailedPrecondition, err.Error())
	case validator.IsValidationError(err):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}
