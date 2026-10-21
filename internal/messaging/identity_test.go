package messaging

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCert generates a self-signed certificate with the given URI SANs and
// writes it (PEM encoded) to a temp file.
func writeCert(t *testing.T, uris []*url.URL) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "node-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		URIs:         uris,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	path := filepath.Join(t.TempDir(), "agent.crt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create cert file: %v", err)
	}
	defer file.Close()

	if err := pem.Encode(file, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("encode certificate: %v", err)
	}
	return path
}

func TestLoadNodeID_ReadsURISAN(t *testing.T) {
	uri, err := url.Parse("spiffe://probecube/node/node-abc")
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}

	got := loadNodeID(writeCert(t, []*url.URL{uri}))
	if want := "node-abc"; got != want {
		t.Fatalf("loadNodeID() = %q, want %q", got, want)
	}
}

func TestLoadNodeID_IgnoresUnrelatedURIs(t *testing.T) {
	other, err := url.Parse("https://example.com/not-a-node")
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}
	want, err := url.Parse("spiffe://probecube/node/node-xyz")
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}

	// The unrelated URI comes first so a naive "first URI wins" implementation
	// would return the wrong value.
	got := loadNodeID(writeCert(t, []*url.URL{other, want}))
	if got != "node-xyz" {
		t.Fatalf("loadNodeID() = %q, want %q", got, "node-xyz")
	}
}

func TestLoadNodeID_NoURISAN(t *testing.T) {
	if got := loadNodeID(writeCert(t, nil)); got != "" {
		t.Fatalf("loadNodeID() = %q, want empty", got)
	}
}

func TestLoadNodeID_MissingFile(t *testing.T) {
	if got := loadNodeID(filepath.Join(t.TempDir(), "does-not-exist.crt")); got != "" {
		t.Fatalf("loadNodeID() = %q, want empty", got)
	}
}

func TestInboxPrefix(t *testing.T) {
	tests := []struct {
		name   string
		sn     string
		nodeID string
		want   string
	}{
		// The SN wins because the callout grants inbox access by SN. Using the
		// uuid here would break every request/reply with a permission violation.
		{name: "prefers sn", sn: "SN-1", nodeID: "node-abc", want: "_INBOX.SN-1"},
		{name: "falls back to node id", sn: "", nodeID: "node-abc", want: "_INBOX.node-abc"},
		{name: "empty when nothing known", sn: "", nodeID: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inboxPrefix(tt.sn, tt.nodeID); got != tt.want {
				t.Fatalf("inboxPrefix(%q, %q) = %q, want %q", tt.sn, tt.nodeID, got, tt.want)
			}
		})
	}
}
