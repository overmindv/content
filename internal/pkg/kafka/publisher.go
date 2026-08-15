package kafka

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
	kafkago "github.com/segmentio/kafka-go"
)

type Config struct {
	Enabled  bool
	Brokers  []string
	Topic    string
	ClientID string
}

type Publisher struct {
	enabled bool
	logger  *slog.Logger
	topic   string
	writer  *kafkago.Writer
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

func NewPublisher(cfg Config, logger *slog.Logger) *Publisher {
	if !cfg.Enabled {
		return &Publisher{logger: logger}
	}

	return &Publisher{
		enabled: true,
		logger:  logger,
		topic:   cfg.Topic,
		writer: &kafkago.Writer{
			Addr:         kafkago.TCP(cfg.Brokers...),
			Topic:        cfg.Topic,
			RequiredAcks: kafkago.RequireOne,
			Async:        true,
			Balancer:     &kafkago.LeastBytes{},
			Transport: &kafkago.Transport{
				ClientID: cfg.ClientID,
			},
		},
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

func (p *Publisher) Close() error {
	if p.writer == nil {
		return nil
	}

	return p.writer.Close()
}

func (p *Publisher) publishContentItem(ctx context.Context, eventName string, item domain.ContentItem) {
	if !p.enabled || p.writer == nil {
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

	err = p.writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte(item.ID),
		Value: payload,
	})
	if err != nil {
		p.logger.Warn("failed to publish kafka event", "error", err, "event", eventName, "topic", p.topic)
	}
}

func (p *Publisher) publishContentRevision(ctx context.Context, eventName string, revision domain.ContentRevision) {
	if !p.enabled || p.writer == nil {
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

	err = p.writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte(revision.ContentItemID),
		Value: payload,
	})
	if err != nil {
		p.logger.Warn("failed to publish kafka event", "error", err, "event", eventName, "topic", p.topic)
	}
}
