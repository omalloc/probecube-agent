package messaging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-kratos/kratos/v3/log"
	"github.com/omalloc/probecube-agent/internal/conf"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), filepath.Base(t.Name()))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestValidateLeafNode_RejectsMissingTLS(t *testing.T) {
	err := validateLeafNode(&conf.LeafNode{
		Endpoint: []string{"tls://nats:4222"},
	})
	if err == nil {
		t.Fatal("expected an error when leafnode.tls is missing, got nil")
	}
}

func TestValidateLeafNode_RejectsEmptyEndpoint(t *testing.T) {
	err := validateLeafNode(&conf.LeafNode{
		Tls: &conf.LeafNode_TLS{CertFile: "a", KeyFile: "b", RootCa: "c"},
	})
	if err == nil {
		t.Fatal("expected an error when leafnode.endpoint is empty, got nil")
	}
}

func TestValidateLeafNode_RejectsMissingFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.pem")
	err := validateLeafNode(&conf.LeafNode{
		Endpoint: []string{"tls://nats:4222"},
		Tls: &conf.LeafNode_TLS{
			CertFile: missing,
			KeyFile:  missing,
			RootCa:   missing,
		},
	})
	if err == nil {
		t.Fatal("expected an error when the referenced TLS files do not exist, got nil")
	}
}

func TestValidateLeafNode_AcceptsCompleteConfig(t *testing.T) {
	path := writeTempFile(t, "placeholder")
	err := validateLeafNode(&conf.LeafNode{
		Endpoint: []string{"tls://nats:4222"},
		Tls: &conf.LeafNode_TLS{
			CertFile: path,
			KeyFile:  path,
			RootCa:   path,
		},
	})
	if err != nil {
		t.Fatalf("validateLeafNode() = %v, want nil", err)
	}
}

func TestNatsErrorHandler_DoesNotPanicOnNilSubscription(t *testing.T) {
	// The NATS library passes a nil subscription for connection-level errors.
	// Reading sub.Subject unconditionally would panic in that path.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("natsErrorHandler panicked: %v", r)
		}
	}()

	log.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	natsErrorHandler(nil, nil, io.EOF)
}
