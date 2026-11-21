package external

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

func (c *MediaClient) SignPublishedAttachment(ctx context.Context, ref domain.PublishedAttachmentRef) (domain.AttachmentDownload, error) {
	endpoint := c.base.JoinPath("v1", "internal", "content", "materials", ref.MaterialID, "revisions", ref.RevisionID, "files", ref.FileID, "download-url")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), nil)
	if err != nil {
		return domain.AttachmentDownload{}, service.ErrMediaUnavailable
	}
	req.Header.Set("X-Media-Service-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return domain.AttachmentDownload{}, service.ErrMediaUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return domain.AttachmentDownload{}, service.ErrNotFound
	case http.StatusOK:
		return readAttachmentDownload(resp.Body)
	default:
		return domain.AttachmentDownload{}, service.ErrMediaUnavailable
	}
}

func readAttachmentDownload(body io.Reader) (domain.AttachmentDownload, error) {
	const maxResponse = 8 * 1024
	data, err := io.ReadAll(io.LimitReader(body, maxResponse+1))
	var result domain.AttachmentDownload
	if err != nil || len(data) > maxResponse || json.Unmarshal(data, &result) != nil {
		return domain.AttachmentDownload{}, service.ErrMediaUnavailable
	}
	parsed, err := url.Parse(result.URL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != schemeHTTP && parsed.Scheme != schemeHTTPS) ||
		parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery == "" || !result.ExpiresAt.After(time.Now()) {
		return domain.AttachmentDownload{}, service.ErrMediaUnavailable
	}

	return result, nil
}
