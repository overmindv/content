package external

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/overmindv/content/internal/pkg/service"
)

type TopicClient struct {
	base *url.URL
	http *http.Client
}

func NewTopicClient(endpoint string) (*TopicClient, error) {
	base, err := url.Parse(endpoint)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("CONTENT_ENTITIES_URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 16

	return &TopicClient{base: base, http: &http.Client{
		Transport: transport,
		Timeout:   2 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}, nil
}

func (c *TopicClient) CheckActiveTopic(ctx context.Context, topicID string) error {
	const maxResponseBytes = 32 * 1024
	endpoint := c.base.JoinPath("v1", "topics", topicID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return service.ErrTopicCatalogUnavailable
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return service.ErrTopicCatalogUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return service.ErrInactiveTopic
	}
	if resp.StatusCode != http.StatusOK {
		return service.ErrTopicCatalogUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return service.ErrTopicCatalogUnavailable
	}
	var topic struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err = json.Unmarshal(data, &topic); err != nil || topic.ID != topicID || topic.Status == "" {
		return service.ErrTopicCatalogUnavailable
	}
	if topic.Status != "active" {
		return service.ErrInactiveTopic
	}

	return nil
}
