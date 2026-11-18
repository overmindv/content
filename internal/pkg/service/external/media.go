package external

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

type MediaClient struct {
	base  *url.URL
	token string
	http  *http.Client
}

func NewMediaClient(address, token string) (*MediaClient, error) {
	base, err := url.Parse(address)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" ||
		base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return nil, errors.New("CONTENT_MEDIA_URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if len(token) < 32 || len(token) > 256 ||
		strings.IndexFunc(token, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		return nil, errors.New("CONTENT_MEDIA_TOKEN must be a separate Media service token")
	}
	return &MediaClient{base: base, token: token, http: &http.Client{
		Timeout:       3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *MediaClient) CheckAttachments(ctx context.Context, actorID string, ids []string) error {
	return c.request(ctx, http.MethodPost, c.base.JoinPath("v1", "internal", "content", "binding-files", "check"), struct {
		ActorID string   `json:"actor_user_id"`
		IDs     []string `json:"file_ids"`
	}{actorID, ids})
}

func (c *MediaClient) ReplaceBinding(ctx context.Context, b domain.MediaBinding) error {
	endpoint := c.base.JoinPath("v1", "internal", "content", "materials", b.MaterialID, "revisions", b.RevisionID, "bindings")
	return c.request(ctx, http.MethodPut, endpoint, struct {
		ActorID string   `json:"actor_user_id"`
		IDs     []string `json:"file_ids"`
		Parent  string   `json:"inherit_from_revision_id,omitempty"`
		Version int16    `json:"version"`
	}{b.ActorID, append([]string{}, b.FileIDs...), b.InheritFromRevisionID, b.Version})
}

func (c *MediaClient) request(ctx context.Context, method string, endpoint *url.URL, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return service.ErrMediaUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return service.ErrMediaUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Media-Service-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return service.ErrMediaUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusConflict, http.StatusNotFound:
		return service.ErrAttachmentNotReady
	default:
		return service.ErrMediaUnavailable
	}
}
