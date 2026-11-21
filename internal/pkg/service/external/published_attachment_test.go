package external

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

func TestMediaPublishedAttachment(t *testing.T) {
	t.Parallel()

	token := strings.Repeat("a", 32)
	expiry := time.Now().Add(30 * time.Second)
	ref := domain.PublishedAttachmentRef{MaterialID: "material", RevisionID: "revision", FileID: "file"}
	const link = "https://objects.invalid/media/file?signature=fixture"
	for _, tc := range []struct {
		name   string
		status int
		body   string
		result domain.AttachmentDownload
		want   error
	}{
		{name: "valid", status: http.StatusOK, result: domain.AttachmentDownload{URL: link, ExpiresAt: expiry}},
		{name: "expired", status: http.StatusOK, result: domain.AttachmentDownload{URL: link, ExpiresAt: expiry.Add(-time.Minute)}, want: service.ErrMediaUnavailable},
		{name: "credentials", status: http.StatusOK, result: domain.AttachmentDownload{URL: "https://user@objects.invalid/file?sig=1", ExpiresAt: expiry}, want: service.ErrMediaUnavailable},
		{name: "oversize", status: http.StatusOK, body: strings.Repeat(" ", 8193), want: service.ErrMediaUnavailable},
		{name: "unbound", status: http.StatusNotFound, want: service.ErrNotFound},
		{name: "unauthorized", status: http.StatusForbidden, want: service.ErrMediaUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/internal/content/materials/material/revisions/revision/files/file/download-url" ||
					r.Header.Get("X-Media-Service-Token") != token {
					t.Error("unexpected internal signing request")
				}
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				} else if tc.status == http.StatusOK {
					_ = json.NewEncoder(w).Encode(tc.result)
				}
			}))
			t.Cleanup(upstream.Close)
			client, err := NewMediaClient(upstream.URL, token)
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.SignPublishedAttachment(t.Context(), ref)
			if !errors.Is(err, tc.want) {
				t.Error("unexpected signing result", err)
			}
			if tc.want == nil {
				if got.URL != link || !got.ExpiresAt.Equal(expiry) {
					t.Error("unexpected link or expiry")
				}
			} else if got.URL != "" {
				t.Error("refusal returned a URL")
			}
		})
	}
}
