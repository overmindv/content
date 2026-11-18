package external

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/overmindv/content/internal/pkg/service"
)

func TestTopicClient(t *testing.T) {
	const topic = "11111111-1111-4111-8111-111111111111"
	for _, tt := range []struct {
		name string
		code int
		body string
		want error
	}{
		{"active", 200, fmt.Sprintf(`{"id":%q,"status":"active"}`, topic), nil},
		{"archived", 200, fmt.Sprintf(`{"id":%q,"status":"archived"}`, topic), service.ErrInactiveTopic},
		{"not found", 404, "", service.ErrInactiveTopic},
		{"unavailable", 503, "", service.ErrTopicCatalogUnavailable},
		{"wrong ID", 200, `{"id":"other","status":"active"}`, service.ErrTopicCatalogUnavailable},
		{"redirect", 302, "", service.ErrTopicCatalogUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/catalog/v1/topics/"+topic {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(tt.code)
				_, _ = fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			client, err := NewTopicClient(server.URL + "/catalog")
			if err != nil {
				t.Fatal(err)
			}
			if err = client.CheckActiveTopic(context.Background(), topic); !errors.Is(err, tt.want) {
				t.Errorf("CheckActiveTopic error = %v, want %v", err, tt.want)
			}
		})
	}
}
