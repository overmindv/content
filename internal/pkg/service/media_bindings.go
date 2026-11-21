package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

type AttachmentChecker interface {
	CheckAttachments(ctx context.Context, actorID string, ids []string) error
}

type BindingDelivery interface {
	ReplaceBinding(context.Context, domain.MediaBinding) error
}

type BindingQueue interface {
	ClaimMediaBinding(context.Context) (domain.MediaBinding, bool, error)
	FinishMediaBinding(context.Context, domain.MediaBinding, time.Duration) error
}

type MediaBindingWorker struct {
	queue BindingQueue
	media BindingDelivery
}

func NewMediaBindingWorker(queue BindingQueue, media BindingDelivery) *MediaBindingWorker {
	return &MediaBindingWorker{queue: queue, media: media}
}

func (w *MediaBindingWorker) DeliverNext(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	intent, found, err := w.queue.ClaimMediaBinding(ctx)
	if err != nil || !found {
		return found, err
	}
	err = w.media.ReplaceBinding(ctx, intent)
	var retry time.Duration
	if err != nil {
		retry = time.Second << min(max(intent.Attempts-1, 0), 6)
	}

	return true, errors.Join(err, w.queue.FinishMediaBinding(ctx, intent, retry))
}

func (w *MediaBindingWorker) Run(ctx context.Context, logger *slog.Logger) error {
	delay := time.NewTimer(0)
	defer delay.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-delay.C:
		}
		for range 8 {
			found, err := w.DeliverNext(ctx)
			if err != nil && ctx.Err() == nil {
				logger.Warn("media binding delivery failed", "error", err)
			}
			if !found || err != nil {
				break
			}
		}
		delay.Reset(250 * time.Millisecond)
	}
}

func checkAttachmentSet(ctx context.Context, checker AttachmentChecker, actorID string, set *domain.AttachmentSet) (*domain.AttachmentSet, error) {
	if set == nil {
		return nil, nil
	}
	if err := validator.ValidateAssetCount(len(set.Assets)); err != nil {
		return nil, err
	}
	assets := normalizeAssets(set.Assets)
	ids := make([]string, 0, len(assets))
	seen := make(map[string]bool, len(assets))
	for i := range assets {
		a := &assets[i]
		a.AssetID = strings.ToLower(a.AssetID)
		if !validProposalID(a.AssetID) || seen[a.AssetID] || len(a.Title) > 256 || a.Position < 0 {
			return nil, validator.Error{Message: "invalid or duplicate attachment"}
		}
		switch a.Kind {
		case domain.AssetKindImage, domain.AssetKindAttachment, domain.AssetKindPDF, domain.AssetKindArchive:
		default:
			return nil, validator.Error{Message: "invalid attachment kind"}
		}
		seen[a.AssetID] = true
		ids = append(ids, a.AssetID)
	}
	if len(ids) != 0 {
		if checker == nil {
			return nil, ErrMediaUnavailable
		}
		if err := checker.CheckAttachments(ctx, actorID, ids); err != nil {
			return nil, err
		}
	}

	return &domain.AttachmentSet{Assets: assets}, nil
}
