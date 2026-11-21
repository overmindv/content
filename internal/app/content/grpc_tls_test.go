package content

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/overmindv/content/internal/config"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	fixtureCertificateFile = "cert.pem"
	fixtureKeyFile         = "key.pem"
)

func TestGRPCServerTLS(t *testing.T) {
	t.Parallel()

	cfg, roots := testGRPCTLSFiles(t)
	tlsConfig, err := loadGRPCTLS(cfg)
	if err != nil || tlsConfig == nil {
		t.Fatal("could not load TLS configuration", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newGRPCServer(NewServer(nil, nil, nil), config.GRPCConfig{ServiceToken: testServiceToken}, grpc.Creds(credentials.NewTLS(tlsConfig)))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	for _, tc := range []struct {
		name      string
		transport credentials.TransportCredentials
		want      codes.Code
	}{
		{"trusted", credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}), codes.Unauthenticated},
		{"unknown CA", credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: x509.NewCertPool()}), codes.Unavailable},
		{"plaintext", insecure.NewCredentials(), codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(tc.transport))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			client := contentpb.NewContentServiceClient(conn)
			_, err = client.GetContentItem(ctx, &contentpb.GetContentItemRequest{})
			if got := status.Code(err); got != tc.want {
				t.Errorf("status = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestGRPCTLSRejectsPartialConfiguration(t *testing.T) {
	t.Parallel()

	for _, cfg := range []config.GRPCConfig{
		{TLSCertFile: fixtureCertificateFile},
		{TLSKeyFile: fixtureKeyFile},
		{TLSCertFile: filepath.Join(t.TempDir(), "missing.pem"), TLSKeyFile: fixtureKeyFile},
	} {
		if result, err := loadGRPCTLS(cfg); err == nil || result != nil {
			t.Error("invalid TLS configuration was accepted")
		}
	}
}

func testGRPCTLSFiles(t *testing.T) (config.GRPCConfig, *x509.CertPool) {
	t.Helper()
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(fixture.Close)
	cert := fixture.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := config.GRPCConfig{TLSCertFile: filepath.Join(dir, fixtureCertificateFile), TLSKeyFile: filepath.Join(dir, fixtureKeyFile)}
	if err := os.WriteFile(cfg.TLSCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.TLSKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(fixture.Certificate())

	return cfg, roots
}
