package content

import (
	"crypto/tls"
	"errors"

	"github.com/overmindv/content/internal/config"
)

func loadGRPCTLS(cfg config.GRPCConfig) (*tls.Config, error) {
	if cfg.TLSCertFile == "" && cfg.TLSKeyFile == "" {
		return nil, nil
	}
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return nil, errors.New("CONTENT_GRPC_TLS_CERT_FILE and CONTENT_GRPC_TLS_KEY_FILE must be set together")
	}
	certificate, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, errors.New("could not load the Content gRPC TLS certificate and key")
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
	}, nil
}
