package external

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
)

var errReviewSource = errors.New("review candidate source unavailable")

type ReviewCandidateClient struct {
	endpoint string
	token    string
	http     *http.Client
}

func NewReviewCandidateClient(address, token string) (*ReviewCandidateClient, error) {
	base, err := url.Parse(address)
	if err != nil || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return nil, errors.New("CONTENT_REVIEW_CANDIDATES_URL must be an absolute URL without credentials, query or fragment")
	}
	if base.Scheme != "https" && (base.Scheme != "http" || !net.ParseIP(base.Hostname()).IsLoopback()) {
		return nil, errors.New("CONTENT_REVIEW_CANDIDATES_URL requires HTTPS outside loopback")
	}
	if token == "" || strings.IndexFunc(token, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		return nil, errors.New("invalid CONTENT_REVIEW_CANDIDATES_TOKEN")
	}
	return &ReviewCandidateClient{
		endpoint: base.JoinPath("v1", "review-candidates").String(),
		token:    token,
		http: &http.Client{
			Timeout:       2 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (c *ReviewCandidateClient) FindReviewCandidates(ctx context.Context, check domain.ReviewCheck) (domain.ReviewCandidates, error) {
	payload, err := json.Marshal(struct {
		TopicID          string   `json:"topic_id"`
		ProposalAuthorID string   `json:"proposal_author_id"`
		MaterialOwnerID  string   `json:"material_owner_id"`
		AssigneeIDs      []string `json:"assignee_ids"`
	}{check.TopicID, check.ProposalAuthorID, check.MaterialOwnerID, check.AssigneeIDs})
	if err != nil {
		return domain.ReviewCandidates{}, errReviewSource
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return domain.ReviewCandidates{}, errReviewSource
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return domain.ReviewCandidates{}, errReviewSource
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return domain.ReviewCandidates{}, errReviewSource
	}
	const maxResponseBytes = 16 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return domain.ReviewCandidates{}, errReviewSource
	}
	var result struct {
		TopicID     string   `json:"topic_id"`
		RankedIDs   []string `json:"ranked_candidate_ids"`
		EligibleIDs []string `json:"eligible_assignee_ids"`
	}
	if err = json.Unmarshal(data, &result); err != nil || result.TopicID != check.TopicID ||
		result.RankedIDs == nil || result.EligibleIDs == nil || len(result.RankedIDs) > 64 || len(result.EligibleIDs) > len(check.AssigneeIDs) {
		return domain.ReviewCandidates{}, errReviewSource
	}
	known := make(map[string]bool, len(check.AssigneeIDs))
	for _, id := range check.AssigneeIDs {
		known[id] = false
	}
	for _, id := range result.EligibleIDs {
		if verified, ok := known[id]; !ok || verified {
			return domain.ReviewCandidates{}, errReviewSource
		}
		known[id] = true
	}
	seen := make(map[string]bool, len(result.RankedIDs))
	for _, raw := range result.RankedIDs {
		id, err := uuid.Parse(raw)
		verified, assigned := known[raw]
		if err != nil || id == uuid.Nil || raw != id.String() || seen[raw] || (assigned && !verified) {
			return domain.ReviewCandidates{}, errReviewSource
		}
		seen[raw] = true
	}
	return domain.ReviewCandidates{RankedIDs: result.RankedIDs, EligibleIDs: result.EligibleIDs}, nil
}
