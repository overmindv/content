//go:build component

package component

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/service/external"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/tests/builders"
)

func TestContentCatalog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, testDatabaseConfig(t))
	if err != nil {
		t.Fatal("could not open test database")
	}
	defer pool.Close()
	store := postgres.NewContentItemStore(pool)
	topic, otherTopic := uuid.NewString(), uuid.NewString()
	entities := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/v1/topics/")
		if id != topic && id != otherTopic {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":%q,"status":"active"}`, id)
	}))
	defer entities.Close()
	client, err := external.NewTopicClient(entities.URL)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewContentItemService(store, client)
	owner, reader := uuid.NewString(), uuid.NewString()
	create := func(actor, topicID string) domain.ContentItem {
		t.Helper()
		input := builders.ContentItem()
		input.CreatedBy = actor
		input.Slug += "-" + uuid.NewString()
		input.Learning = &domain.LearningMetadata{TopicIDs: []string{strings.ToUpper(topicID), topicID}, EstimatedMinutes: 25,
			LearningOutcomes: []string{" Explain usage "}}
		item, err := svc.Create(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if item.Learning == nil || len(item.Learning.TopicIDs) != 1 || item.Learning.TopicIDs[0] != topicID ||
			item.Learning.EstimatedMinutes != 25 || len(item.Learning.LearningOutcomes) != 1 || item.Learning.LearningOutcomes[0] != "Explain usage" {
			t.Errorf("learning metadata after create: %+v", item.Learning)
		}
		return item
	}
	invalid := builders.ContentItem()
	invalid.CreatedBy, invalid.Slug = owner, "unknown-topic-"+uuid.NewString()
	invalid.Learning = &domain.LearningMetadata{TopicIDs: []string{uuid.NewString()}}
	if _, err = svc.Create(ctx, invalid); !errors.Is(err, service.ErrInactiveTopic) {
		t.Errorf("create with unknown topic: %v", err)
	}
	first, second, other, draft := create(owner, topic), create(reader, topic), create(owner, otherTopic), create(owner, topic)
	items := []domain.ContentItem{first, second, other, draft}
	defer func() {
		for _, item := range items {
			if _, err := store.Delete(ctx, item.ID, item.CreatedBy); err != nil {
				t.Error(err)
			}
		}
	}()
	meta := domain.UpdateContentItemInput{ID: draft.ID, Type: draft.Type, Status: draft.Status, Title: draft.Title, Slug: draft.Slug, UpdatedBy: owner}
	foreign := meta
	foreign.UpdatedBy = reader
	foreign.Learning = &domain.LearningMetadata{TopicIDs: []string{otherTopic}}
	if _, err = svc.Update(ctx, foreign); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("update of another author's learning metadata = %v, want NotFound", err)
	}
	unchanged, err := svc.Update(ctx, meta)
	if err != nil || unchanged.Learning == nil || len(unchanged.Learning.TopicIDs) != 1 ||
		unchanged.Learning.TopicIDs[0] != topic {
		t.Errorf("draft bindings did not stay unchanged: %v", err)
	}
	meta.Learning = &domain.LearningMetadata{}
	cleared, err := svc.Update(ctx, meta)
	if err != nil || cleared.Learning != nil {
		t.Errorf("empty learning field did not clear metadata: %v", err)
	}
	meta.Learning.TopicIDs = []string{topic}
	if _, err = svc.Update(ctx, meta); err != nil {
		t.Fatal(err)
	}
	for _, item := range items[:3] {
		if _, err = svc.Publish(ctx, domain.ChangeContentItemStateInput{ID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, ActorID: item.CreatedBy}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = svc.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID: first.ID, ExpectedRevisionID: first.CurrentRevisionID, CreatedBy: owner, Source: "# Private new revision",
	}); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if _, err = pool.Exec(ctx, `UPDATE content_items SET created_at = '2026-01-01T00:00:00Z' WHERE id = $1`, item.ID); err != nil {
			t.Fatal(err)
		}
	}
	filter := domain.CatalogFilter{ActorID: reader, TopicID: topic, Status: domain.ContentStatusPublished, PageSize: 1}
	page1, cursor, err := svc.Catalog(ctx, filter, "")
	if err != nil || len(page1) != 1 || cursor == "" {
		t.Fatalf("first catalog page: %+v, cursor present = %v, error = %v", page1, cursor != "", err)
	}
	page2, next, err := svc.Catalog(ctx, filter, cursor)
	if err != nil || len(page2) != 1 || next != "" || page1[0].ID == page2[0].ID {
		t.Fatalf("second catalog page: %+v, duplicate or extra cursor, error = %v", page2, err)
	}
	for _, entry := range []domain.CatalogEntry{page1[0], page2[0]} {
		if entry.ID != first.ID && entry.ID != second.ID || len(entry.Learning.TopicIDs) != 1 || entry.Learning.TopicIDs[0] != topic {
			t.Errorf("catalog returned a nonmatching entry: %+v", entry)
		}
	}
	filter.ActorID, filter.AuthorID, filter.PageSize = owner, owner, 20
	ownedPublished, _, err := svc.Catalog(ctx, filter, "")
	if err != nil || len(ownedPublished) != 1 || ownedPublished[0].RevisionID != first.CurrentRevisionID {
		t.Errorf("author filter used a draft revision: %+v, %v", ownedPublished, err)
	}
	filter.ActorID, filter.Status = reader, domain.ContentStatusDraft
	hidden, _, err := svc.Catalog(ctx, filter, "")
	if err != nil || len(hidden) != 0 {
		t.Errorf("catalog exposed another author's draft: %+v, %v", hidden, err)
	}
	filter.ActorID = owner
	visible, _, err := svc.Catalog(ctx, filter, "")
	if err != nil || len(visible) != 1 || visible[0].ID != draft.ID {
		t.Errorf("author draft catalog: %+v, %v", visible, err)
	}
}
