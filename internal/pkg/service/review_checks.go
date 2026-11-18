package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
)

type ReviewCheckStore interface {
	ClaimReviewCheck(context.Context) (domain.ReviewCheck, bool, error)
	ApplyReviewCheck(context.Context, domain.ReviewCheck, domain.ReviewCandidates) error
}

type ReviewCandidateSource interface {
	FindReviewCandidates(context.Context, domain.ReviewCheck) (domain.ReviewCandidates, error)
}

type ReviewCheckWorker struct {
	store  ReviewCheckStore
	source ReviewCandidateSource
}

func NewReviewCheckWorker(store ReviewCheckStore, source ReviewCandidateSource) *ReviewCheckWorker {
	return &ReviewCheckWorker{store: store, source: source}
}

func (w *ReviewCheckWorker) CheckNext(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	check, found, err := w.store.ClaimReviewCheck(ctx)
	if err != nil || !found || check.TopicID == "" {
		return found, err
	}
	result, err := w.source.FindReviewCandidates(ctx, check)
	if err != nil {
		return true, err
	}
	if err = w.store.ApplyReviewCheck(ctx, check, result); err != nil {
		return true, fmt.Errorf("apply review check: %w", err)
	}
	return true, nil
}

func (w *ReviewCheckWorker) Run(ctx context.Context, logger *slog.Logger) error {
	delay := time.NewTimer(0)
	defer delay.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-delay.C:
		}
		for range 16 {
			found, err := w.CheckNext(ctx)
			if err != nil && ctx.Err() == nil {
				logger.Warn("review candidate check failed")
			}
			if !found || err != nil {
				break
			}
		}
		delay.Reset(250 * time.Millisecond)
	}
}
