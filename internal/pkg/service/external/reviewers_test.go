package external

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
)

const reviewFixtureToken = "fixture-review-candidates-service"

func TestReviewCandidateClient(t *testing.T) {
	t.Parallel()

	topic, author, assignee, candidate := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	check := domain.ReviewCheck{TopicID: topic, ProposalAuthorID: author, AssigneeIDs: []string{assignee}}
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{"valid", fmt.Sprintf(`{"topic_id":%q,"ranked_candidate_ids":[%q],"eligible_assignee_ids":[%q]}`, topic, candidate, assignee), true},
		{"wrong topic", fmt.Sprintf(`{"topic_id":%q,"ranked_candidate_ids":[],"eligible_assignee_ids":[]}`, candidate), false},
		{"missing eligibility", fmt.Sprintf(`{"topic_id":%q,"ranked_candidate_ids":[]}`, topic), false},
		{"unexpected assignee", fmt.Sprintf(`{"topic_id":%q,"ranked_candidate_ids":[],"eligible_assignee_ids":[%q]}`, topic, candidate), false},
		{"revoked candidate", fmt.Sprintf(`{"topic_id":%q,"ranked_candidate_ids":[%q],"eligible_assignee_ids":[]}`, topic, assignee), false},
		{"proposal author", fmt.Sprintf(`{"topic_id":%q,"ranked_candidate_ids":[%q],"eligible_assignee_ids":[]}`, topic, author), false},
		{"oversized", strings.Repeat(" ", 17*1024), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/review-candidates" || r.Header.Get("Authorization") != "Bearer "+reviewFixtureToken {
					t.Error("unexpected candidate request")
				}
				var payload struct {
					TopicID     string   `json:"topic_id"`
					AssigneeIDs []string `json:"assignee_ids"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.TopicID != topic ||
					len(payload.AssigneeIDs) != 1 || payload.AssigneeIDs[0] != assignee {
					t.Error("invalid request payload")
				}
				_, _ = fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			client, err := NewReviewCandidateClient(server.URL, reviewFixtureToken)
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.FindReviewCandidates(t.Context(), check)
			if tt.ok {
				if err != nil || len(result.RankedIDs) != 1 || result.RankedIDs[0] != candidate ||
					len(result.EligibleIDs) != 1 || result.EligibleIDs[0] != assignee {
					t.Errorf("unexpected results: %+v %v", result, err)
				}
			} else if err == nil {
				t.Error("invalid response was accepted")
			}
		})
	}
}

func TestReviewCandidateClientDoesNotFollowRedirect(t *testing.T) {
	t.Parallel()

	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("redirect was followed")
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := NewReviewCandidateClient(server.URL, reviewFixtureToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.FindReviewCandidates(t.Context(), domain.ReviewCheck{}); err == nil {
		t.Error("redirect was accepted")
	}
	if _, err = NewReviewCandidateClient("http://example.test", reviewFixtureToken); err == nil {
		t.Error("remote HTTP allowed a service token")
	}
	if _, err = NewReviewCandidateClient(server.URL, "short"); err == nil {
		t.Error("short service token accepted")
	}
}
