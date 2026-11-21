//go:build component

package component

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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

const mediaTestToken = "content-media-binding-test-token-fixture"

type topicFixture string

func (id topicFixture) CheckActiveTopic(_ context.Context, topic string) error {
	if string(id) != topic {
		return service.ErrInactiveTopic
	}
	return nil
}

func TestRevisionMediaBindings(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	owner, contributor, topic, originalFile, anotherFile := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	unverifiedFile := uuid.NewString()
	media, unavailable, active := mediaBindingFixture(t, ctx,
		map[string]string{originalFile: owner, anotherFile: contributor, unverifiedFile: owner}, unverifiedFile)
	store := postgres.NewContentItemStore(pool, postgres.WithManagedAttachments())
	items, proposals := service.NewContentItemService(store, topicFixture(topic)), service.NewChangeProposalService(store)
	items.SetAttachmentChecker(media)
	proposals.SetAttachmentChecker(media)
	worker := service.NewMediaBindingWorker(store, media)
	deliver := func() {
		t.Helper()
		if found, err := worker.DeliverNext(ctx); !found || err != nil {
			t.Fatal("delivery", found, err)
		}
	}
	input := builders.ContentItem()
	input.CreatedBy, input.Slug = owner, input.Slug+"-"+uuid.NewString()
	input.Learning = &domain.LearningMetadata{TopicIDs: []string{topic}}
	input.Assets = []domain.CreateContentAssetInput{{AssetID: anotherFile, Kind: domain.AssetKindAttachment}}
	if _, err = items.Create(ctx, input); !errors.Is(err, service.ErrAttachmentNotReady) {
		t.Errorf("foreign attachment accepted: %v", err)
	}
	input.Assets[0].AssetID = unverifiedFile
	if _, err = items.Create(ctx, input); !errors.Is(err, service.ErrAttachmentNotReady) {
		t.Errorf("unverified attachment accepted: %v", err)
	}
	input.Assets[0].AssetID = originalFile
	item, err := items.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	state := domain.ChangeContentItemStateInput{ID: item.ID, ExpectedRevisionID: item.CurrentRevisionID, ActorID: owner}
	if _, err = items.Publish(ctx, state); !errors.Is(err, service.ErrAttachmentsPending) {
		t.Errorf("published while attachment was pending: %v", err)
	}
	unavailable.Store(true)
	if found, err := worker.DeliverNext(ctx); !found || !errors.Is(err, service.ErrMediaUnavailable) {
		t.Errorf("delivery failure found=%t: %v", found, err)
	}
	var attempts int
	var waiting bool
	if err = pool.QueryRow(ctx, `
		SELECT attempts, applied_version = 0 AND next_attempt_at > statement_timestamp()
		FROM content_media_bindings WHERE revision_id = $1
	`, item.CurrentRevisionID).Scan(&attempts, &waiting); err != nil || attempts != 1 || !waiting {
		t.Errorf("durable retry attempts=%d waiting=%t: %v", attempts, waiting, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE content_media_bindings SET next_attempt_at = NOW() WHERE revision_id = $1`, item.CurrentRevisionID); err != nil {
		t.Fatal(err)
	}
	unavailable.Store(false)
	deliver()
	if _, err = items.Publish(ctx, state); err != nil {
		t.Fatal(err)
	}
	p, err := proposals.Open(ctx, domain.OpenChangeProposalInput{
		ContentItemID: item.ID, BaseRevisionID: item.CurrentRevisionID, AuthorID: contributor, Source: "# New material version",
	})
	if err != nil || len(p.Head.Assets) != 1 || p.Head.Assets[0].AssetID != originalFile {
		t.Fatal("proposal lost the base attachment", err)
	}
	deliver()
	appended, err := proposals.Append(ctx, domain.AppendChangeProposalRevisionInput{
		ProposalID: p.ID, ExpectedHeadID: p.Head.ID, AuthorID: contributor, Source: "# Keep the same file",
	})
	if err != nil || len(appended.Head.Assets) != 1 || appended.Head.Assets[0].AssetID != originalFile {
		t.Fatal("proposal did not inherit head attachments", err)
	}
	deliver()
	if count := active(originalFile); count != 3 {
		t.Errorf("expected one reference from the publication and two from the proposal, got %d", count)
	}
	draft, err := items.CreateRevision(ctx, domain.CreateContentRevisionInput{
		ContentItemID: item.ID, CreatedBy: owner, ExpectedRevisionID: item.CurrentRevisionID, Source: "# Without files",
		Attachments: &domain.AttachmentSet{},
	})
	if err != nil {
		t.Fatal(err)
	}
	owned, err := store.Get(ctx, item.ID, owner)
	if err != nil || owned.CurrentRevisionID != draft.ID || len(owned.Assets) != 0 {
		t.Error("the explicitly emptied draft still has attachments", err)
	}
	visible, err := items.GetPublished(ctx, item.ID)
	if err != nil || len(visible.Assets) != 1 || visible.Assets[0].RevisionID != item.CurrentRevisionID {
		t.Errorf("public attachments are not scoped to the published revision: %+v %v", visible.Assets, err)
	}
	if _, err = proposals.Close(ctx, p.ID, owner); !errors.Is(err, service.ErrProposalNotFound) {
		t.Error("material owner could close another user's proposal", err)
	}
	abandoned, err := proposals.Append(ctx, domain.AppendChangeProposalRevisionInput{
		ProposalID: p.ID, ExpectedHeadID: appended.Head.ID, AuthorID: contributor, Source: "# Abandoned revision",
		Attachments: &domain.AttachmentSet{Assets: []domain.CreateContentAssetInput{{AssetID: anotherFile, Kind: domain.AssetKindAttachment}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	old, found, err := store.ClaimMediaBinding(ctx)
	if err != nil || !found || old.RevisionID != abandoned.Head.ID {
		t.Fatal("claim old binding", found, err)
	}
	if _, err = proposals.Close(ctx, p.ID, contributor); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		deliver()
	}
	if err = media.ReplaceBinding(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err = store.FinishMediaBinding(ctx, old, 0); err != nil {
		t.Fatal(err)
	}
	if active(anotherFile) != 0 || active(originalFile) != 1 {
		t.Error("closing a proposal restored a stale binding or released a shared file")
	}
	input.Slug += "-shared"
	other, err := items.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	deliver()
	if _, err = store.Delete(ctx, item.ID, owner); err != nil {
		t.Fatal(err)
	}
	deliver()
	if active(originalFile) != 1 {
		t.Error("deleting one material released a file still referenced by another")
	}
	if _, err = store.Delete(ctx, other.ID, owner); err != nil {
		t.Fatal(err)
	}
	deliver()
	if active(originalFile) != 0 {
		t.Error("material deletion did not release the remaining references")
	}
}

func mediaBindingFixture(t *testing.T, ctx context.Context, owners map[string]string, unverified string) (*external.MediaClient, *atomic.Bool, func(string) int) {
	t.Helper()
	unavailable := &atomic.Bool{}
	var handler http.Handler
	var active func(string) int
	if address := os.Getenv("COMPONENT_TEST_MEDIA_URL"); address != "" {
		backend, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}
		pool, err := pgxpool.New(ctx, os.Getenv("COMPONENT_TEST_MEDIA_DSN"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		for id, owner := range owners {
			if _, err := pool.Exec(ctx, `
				WITH b AS (
				    INSERT INTO blobs (id,checksum_sha256,size_bytes,visibility,detected_content_type,object_key,scan_status,scan_verified)
				    VALUES (gen_random_uuid(),$3,8,'private','application/pdf',$3,'clean',$4) RETURNING id
				)
				INSERT INTO files (id,blob_id,owner_user_id,purpose,visibility,original_name,declared_content_type,size_bytes,checksum_sha256,status)
				SELECT $1,b.id,$2,'attachment','private','fixture.pdf','application/pdf',8,$3,'ready' FROM b
			`, id, owner, "content-test-"+id, id != unverified); err != nil {
				t.Fatal(err)
			}
		}
		handler = httputil.NewSingleHostReverseProxy(backend)
		active = func(id string) int {
			t.Helper()
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM file_bindings WHERE file_id = $1 AND deleted_at IS NULL`, id).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
	} else {
		var mu sync.Mutex
		bindings := map[string]domain.MediaBinding{}
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				ActorID string   `json:"actor_user_id"`
				IDs     []string `json:"file_ids"`
				Source  string   `json:"inherit_from_revision_id"`
				Version int16    `json:"version"`
			}
			if r.Header.Get("X-Media-Service-Token") != mediaTestToken || json.NewDecoder(r.Body).Decode(&body) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			var material, revision string
			if r.Method == http.MethodPut && len(parts) == 8 {
				material, revision = parts[4], parts[6]
				if bindings[revision].Version >= body.Version {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			} else if r.Method != http.MethodPost || r.URL.Path != "/v1/internal/content/binding-files/check" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			parent := bindings[body.Source]
			for _, id := range body.IDs {
				if id == unverified || owners[id] != body.ActorID && !(parent.MaterialID == material && slices.Contains(parent.FileIDs, id)) {
					w.WriteHeader(http.StatusConflict)
					return
				}
			}
			if revision != "" {
				bindings[revision] = domain.MediaBinding{MaterialID: material, Version: body.Version, FileIDs: body.IDs}
			}
			w.WriteHeader(http.StatusNoContent)
		})
		active = func(id string) int {
			mu.Lock()
			defer mu.Unlock()
			var n int
			for _, b := range bindings {
				if slices.Contains(b.FileIDs, id) {
					n++
				}
			}
			return n
		}
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() && r.Method == http.MethodPut {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	client, err := external.NewMediaClient(proxy.URL, mediaTestToken)
	if err != nil {
		t.Fatal(err)
	}
	return client, unavailable, active
}
