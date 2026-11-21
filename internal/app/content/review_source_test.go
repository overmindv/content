package content

import (
	"testing"

	"github.com/overmindv/content/internal/config"
)

func TestReviewCandidateSourceConfig(t *testing.T) {
	t.Parallel()

	const endpoint = "https://review-source.example.org"
	const token = "fixture-review-source-service-token"
	conf := config.Config{}
	if source, err := reviewCandidateSource(conf); err != nil || source != nil {
		t.Error("empty config must not enable review checks", err)
	}
	conf.ReviewCandidates.URL = endpoint
	if _, err := reviewCandidateSource(conf); err == nil {
		t.Error("URL alone enabled review checks")
	}
	conf.ReviewCandidates.Token = token
	conf.MediaToken = token
	if _, err := reviewCandidateSource(conf); err == nil {
		t.Error("review source must not reuse the Media token")
	}
	conf.MediaToken = ""
	if source, err := reviewCandidateSource(conf); err != nil || source == nil {
		t.Error("valid source was rejected", err)
	}
}
