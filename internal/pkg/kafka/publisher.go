package kafka

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/overmindv/bumblebee/internal/pkg/domain"
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

type templateItemEvent struct {
	Event      string              `json:"event"`
	OccurredAt time.Time           `json:"occurred_at"`
	Item       domain.TemplateItem `json:"item"`
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

func (p *Publisher) PublishCreated(ctx context.Context, item domain.TemplateItem) {
	p.publish(ctx, "template_item.created", item)
}

func (p *Publisher) PublishUpdated(ctx context.Context, item domain.TemplateItem) {
	p.publish(ctx, "template_item.updated", item)
}

func (p *Publisher) PublishDeleted(ctx context.Context, item domain.TemplateItem) {
	p.publish(ctx, "template_item.deleted", item)
}

func (p *Publisher) Close() error {
	if p.writer == nil {
		return nil
	}

	return p.writer.Close()
}

func (p *Publisher) publish(ctx context.Context, eventName string, item domain.TemplateItem) {
	if !p.enabled || p.writer == nil {
		return
	}

	payload, err := json.Marshal(templateItemEvent{
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
