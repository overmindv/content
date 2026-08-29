package kafka

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
)

// Producer — минимальный контракт публикации записей, реализуемый *parker.Producer.
type Producer interface {
	Publish(ctx context.Context, topic, key string, value []byte) error
}

type Config struct {
	Enabled bool
	Topic   string
}

type Publisher struct {
	enabled  bool
	logger   *slog.Logger
	topic    string
	producer Producer
}

type contentItemEvent struct {
	Event      string             `json:"event"`
	OccurredAt time.Time          `json:"occurred_at"`
	Item       domain.ContentItem `json:"item"`
}

type contentRevisionEvent struct {
	Event      string                 `json:"event"`
	OccurredAt time.Time              `json:"occurred_at"`
	Revision   domain.ContentRevision `json:"revision"`
}

func NewPublisher(cfg Config, producer Producer, logger *slog.Logger) *Publisher {
	if !cfg.Enabled {
		return &Publisher{logger: logger}
	}

	return &Publisher{
		enabled:  true,
		logger:   logger,
		topic:    cfg.Topic,
		producer: producer,
	}
}

func (p *Publisher) PublishCreated(ctx context.Context, item domain.ContentItem) {
	p.publishContentItem(ctx, "content_item.created", item)
}

func (p *Publisher) PublishUpdated(ctx context.Context, item domain.ContentItem) {
	p.publishContentItem(ctx, "content_item.updated", item)
}

func (p *Publisher) PublishDeleted(ctx context.Context, item domain.ContentItem) {
	p.publishContentItem(ctx, "content_item.deleted", item)
}

func (p *Publisher) PublishRevisionCreated(ctx context.Context, revision domain.ContentRevision) {
	p.publishContentRevision(ctx, "content_revision.created", revision)
}

func (p *Publisher) publishContentItem(ctx context.Context, eventName string, item domain.ContentItem) {
	if !p.enabled || p.producer == nil {
		return
	}

	payload, err := json.Marshal(contentItemEvent{
		Event:      eventName,
		OccurredAt: time.Now().UTC(),
		Item:       item,
	})
	if err != nil {
		p.logger.Warn("failed to marshal kafka event", "error", err, "event", eventName)
		return
	}

	err = p.producer.Publish(ctx, p.topic, item.ID, payload)
	if err != nil {
		p.logger.Warn("failed to publish kafka event", "error", err, "event", eventName, "topic", p.topic)
	}
}

func (p *Publisher) publishContentRevision(ctx context.Context, eventName string, revision domain.ContentRevision) {
	if !p.enabled || p.producer == nil {
		return
	}

	payload, err := json.Marshal(contentRevisionEvent{
		Event:      eventName,
		OccurredAt: time.Now().UTC(),
		Revision:   revision,
	})
	if err != nil {
		p.logger.Warn("failed to marshal kafka event", "error", err, "event", eventName)
		return
	}

	err = p.producer.Publish(ctx, p.topic, revision.ContentItemID, payload)
	if err != nil {
		p.logger.Warn("failed to publish kafka event", "error", err, "event", eventName, "topic", p.topic)
	}
}
