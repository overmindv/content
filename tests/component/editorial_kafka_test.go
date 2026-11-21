//go:build component

package component

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/pkg/api/activity"
	"github.com/overmindv/parker"
)

func TestEditorialActivityKafkaReplay(t *testing.T) {
	address := os.Getenv("COMPONENT_TEST_KAFKA_BROKERS")
	if address == "" {
		t.Skip("COMPONENT_TEST_KAFKA_BROKERS is not configured")
	}
	topic := activity.DefaultContributionTopic
	f := newMergeFixture(t, postgres.WithOutbox(false, ""), postgres.WithMaterialActivity("", ""), postgres.WithEditorialActivity(topic))
	proposal := f.proposal(uuid.NewString(), "# Editorial Kafka fixture", nil)
	f.approve(proposal)
	result, err := f.merge(proposal)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	brokers := strings.Split(address, ",")
	logger := slog.New(slog.DiscardHandler)
	producer, err := parker.NewProducer(brokers)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	records := make(chan []byte, 4)
	subscriber, err := parker.NewSubscriber(brokers, "editorial-fixture-"+uuid.NewString(), []string{topic},
		func(ctx context.Context, key string, value []byte) error {
			if key == f.item.ID {
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
	dispatch := parker.NewOutboxDispatcher(parker.NewPgOutbox(f.pool), producer, 20*time.Millisecond, logger)
	pubDone, subDone := make(chan error, 1), make(chan error, 1)
	go func() { pubDone <- dispatch.Run(ctx) }()
	go func() { subDone <- subscriber.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-pubDone; err != nil {
			t.Error(err)
		}
		if err := <-subDone; err != nil {
			t.Error(err)
		}
	}()

	receive := func() []byte {
		t.Helper()
		select {
		case data := <-records:
			return data
		case <-ctx.Done():
			t.Fatal("editorial event not received from Kafka")

			return nil
		}
	}
	var authorID string
	var original []byte
	seen := map[string]bool{}
	for range 3 {
		value := receive()
		var event activity.Event[activity.EditorialContribution]
		if err := json.Unmarshal(value, &event); err != nil || event.RevisionID != result.PublishedRevisionID ||
			event.AggregateID != f.item.ID || event.EventID == "" || seen[event.EventID] {
			t.Fatal("invalid or duplicate editorial event", err)
		}
		seen[event.EventID] = true
		if event.EventType == activity.ContributionMerged {
			authorID, original = event.EventID, value
		}
	}
	if authorID == "" {
		t.Fatal("missing author event")
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		tag, err := f.pool.Exec(ctx, `
			UPDATE outbox SET status = 'pending', sent_at = NULL WHERE id = $1 AND status = 'sent'
		`, authorID)
		if err != nil {
			t.Fatal(err)
		}
		if tag.RowsAffected() == 1 {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("editorial row did not reach sent state")
		}
	}
	if replayed := receive(); !bytes.Equal(original, replayed) {
		t.Error("editorial replay changed the Kafka message")
	}
}
