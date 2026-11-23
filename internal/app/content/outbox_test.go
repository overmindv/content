package content

import (
	"context"
	"testing"
	"time"
)

func TestPublishingOutboxDoesNotEnableRetention(t *testing.T) {
	t.Parallel()

	if _, ok := any(publishingOutbox{}).(interface {
		CleanupSent(context.Context, time.Duration, int) (int64, error)
	}); ok {
		t.Fatal("publishing must not enable outbox retention")
	}
}
