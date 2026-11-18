//go:build component

package component

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/pkg/api/activity"
	"github.com/overmindv/parker"
)

func TestMaterialActivityKafkaReplay(t *testing.T) {
	address := os.Getenv("COMPONENT_TEST_KAFKA_BROKERS")
	if address == "" {
		t.Skip("COMPONENT_TEST_KAFKA_BROKERS is not configured")
	}
	f := newActivityFixture(t)
	f.material = activity.DefaultMaterialTopic
	f.store = postgres.NewContentItemStore(f.pool, postgres.WithActivityOutbox(f.material, "", ""))
	item := f.state(t, f.create(t), domain.ContentStatusPublished)
	brokers := strings.Split(address, ",")
	producer, err := parker.NewProducer(brokers)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	records := make(chan []byte, 2)
	subscriber, err := parker.NewSubscriber(brokers, "content-replay-"+uuid.NewString(), []string{f.material},
		func(ctx context.Context, key string, value []byte) error {
			if key == item.ID {
				select {
				case records <- bytes.Clone(value):
				case <-ctx.Done():
				}
			}
			return nil
		}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer subscriber.Close()

	ctx, cancel := context.WithCancel(f.ctx)
	subDone, pubDone := make(chan error, 1), make(chan error, 1)
	go func() { subDone <- subscriber.Run(ctx) }()
	dispatch := parker.NewOutboxDispatcher(parker.NewPgOutbox(f.pool), producer, 20*time.Millisecond, logger)
	go func() { pubDone <- dispatch.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-subDone; err != nil {
			t.Error(err)
		}
		if err := <-pubDone; err != nil {
			t.Error(err)
		}
	}()
	receive := func() []byte {
		t.Helper()
		select {
		case data := <-records:
			return data
		case <-ctx.Done():
			t.Fatal("timed out waiting for Kafka activity")
			return nil
		}
	}
	original := receive()
	var event activity.Event[activity.MaterialState]
	if err := json.Unmarshal(original, &event); err != nil || event.EventType != activity.MaterialPublished ||
		event.RevisionID != item.PublishedRevisionID || event.EventID == "" {
		t.Fatalf("invalid Kafka event: %+v %v", event, err)
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		tag, err := f.pool.Exec(ctx, `
			UPDATE outbox SET status = 'pending', sent_at = NULL WHERE id = $1 AND status = 'sent'
		`, event.EventID)
		if err != nil {
			t.Fatal(err)
		}
		if tag.RowsAffected() == 1 {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("outbox row was not marked sent")
		}
	}
	if replayed := receive(); !bytes.Equal(original, replayed) {
		t.Error("outbox redelivery changed the Kafka message")
	}
}
