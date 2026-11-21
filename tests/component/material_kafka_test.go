//go:build component

package component

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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
	f.store = postgres.NewContentItemStore(f.pool, postgres.WithMaterialActivity(f.material, ""))
	item := f.state(t, f.create(t), domain.ContentStatusPublished)
	brokers := strings.Split(address, ",")
	producer, err := parker.NewProducer(brokers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(producer.Close)
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
	t.Cleanup(subscriber.Close)

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

	if endpoint := os.Getenv("COMPONENT_TEST_FEED_URL"); endpoint != "" {
		feedURL, parseErr := url.Parse(endpoint)
		if parseErr != nil || feedURL.Scheme != "http" || feedURL.Hostname() != "127.0.0.1" ||
			feedURL.User != nil || feedURL.RawQuery != "" || feedURL.Fragment != "" {
			t.Fatal("expected a local Feed test endpoint")
		}
		feedURL.Path = "/v1/feed"
		feedToken := os.Getenv("COMPONENT_TEST_FEED_TOKEN")
		checkFeedMaterial(t, ctx, feedURL, feedToken, item.ID, true)
		f.state(t, item, domain.ContentStatusArchived)
		var archive activity.Event[activity.MaterialState]
		if err = json.Unmarshal(receive(), &archive); err != nil || archive.EventType != activity.MaterialArchived ||
			!archive.Payload.Tombstone || archive.AggregateVersion <= event.AggregateVersion {
			t.Fatal("expected a newer tombstone", err)
		}
		checkFeedMaterial(t, ctx, feedURL, feedToken, item.ID, false)
	}
}

func checkFeedMaterial(t *testing.T, ctx context.Context, endpoint *url.URL, token, materialID string, visible bool) {
	t.Helper()
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Feed-Service-Token", token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal("Feed request failed")
		}
		var body struct {
			Items []struct {
				Href        string `json:"href"`
				Title       string `json:"title"`
				Text        string `json:"text"`
				ActorUserID string `json:"actorUserId"`
			} `json:"items"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || decodeErr != nil {
			t.Fatal("unexpected Feed response", resp.StatusCode, decodeErr)
		}
		found := false
		for _, item := range body.Items {
			if item.Href == "/materials/"+materialID {
				found = true
				if item.Title == "" || item.Text != "" || item.ActorUserID != "" {
					t.Error("Feed material card contains private fields or no public title")
				}
			}
		}
		if found == visible {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("Feed did not apply the material lifecycle event")
		}
	}
}
