package external

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/overmindv/content/internal/pkg/service"
)

func TestMediaClientRejectsRedirect(t *testing.T) {
	t.Parallel()

	var visits atomic.Int32
	untrusted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		visits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(untrusted.Close)
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, untrusted.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(media.Close)

	client, err := NewMediaClient(media.URL, strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err = client.CheckAttachments(t.Context(), "fixture-user", []string{"fixture-file"}); !errors.Is(err, service.ErrMediaUnavailable) {
		t.Error("unexpected redirect result", err)
	}
	if visits.Load() != 0 {
		t.Error("Media request followed the redirect")
	}
}

func TestMediaClientRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	token := strings.Repeat("m", 32)
	for _, cfg := range []struct{ url, token string }{
		{"", token},
		{"https://user@example.invalid", token},
		{"https://example.invalid?scope=other", token},
		{"https://example.invalid", ""},
		{"https://example.invalid", token + "\n"},
	} {
		if _, err := NewMediaClient(cfg.url, cfg.token); err == nil {
			t.Error("accepted invalid Media config")
		}
	}
}
