package content

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/overmindv/content/internal/config"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type readinessChecker interface {
	PingContext(ctx context.Context) error
}

func NewHTTPHandler(cfg config.Config, checker readinessChecker) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"service": cfg.Service.Name,
			"version": cfg.Service.Version,
			"env":     cfg.Service.Environment,
		})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": cfg.Service.Name,
		})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := checker.PingContext(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not-ready",
				"reason": err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ready",
			"service": cfg.Service.Name,
		})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
