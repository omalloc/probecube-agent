package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/omalloc/probecube-agent/internal/conf"
)

func TestEnsureCertificate_SkipsWhenCertPresent(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "agent.crt")
	keyFile := filepath.Join(dir, "agent.key")
	if err := os.WriteFile(certFile, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()

	err := EnsureCertificate(context.Background(), &conf.LeafNode{
		Tls:        &conf.LeafNode_TLS{CertFile: certFile, KeyFile: keyFile, RootCa: filepath.Join(dir, "ca.pem")},
		Enrollment: &conf.Enrollment{Url: srv.URL},
	}, "SN-1")
	if err != nil {
		t.Fatalf("EnsureCertificate() error = %v", err)
	}
	if called {
		t.Fatal("enrollment endpoint was contacted even though a certificate already existed")
	}
}

func TestEnsureCertificate_SkipsRelativeCertAndKey(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("enrollment endpoint must not be contacted when a certificate exists")
	}))
	defer srv.Close()

	certFile := filepath.Join(dir, "agent.crt")
	if err := os.WriteFile(certFile, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "agent.key"), []byte("key"), 0o600)

	if err := EnsureCertificate(context.Background(), &conf.LeafNode{
		Tls:        &conf.LeafNode_TLS{CertFile: certFile, KeyFile: filepath.Join(dir, "agent.key"), RootCa: filepath.Join(dir, "ca.pem")},
		Enrollment: &conf.Enrollment{Url: srv.URL},
	}, "SN-1"); err != nil {
		t.Fatalf("EnsureCertificate() error = %v", err)
	}
}

func TestEnsureCertificate_EnrollsAndWritesFiles(t *testing.T) {
	dir := t.TempDir()

	var got enrollmentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(enrollmentResponse{
			CertPEM: "-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----\n",
			KeyPEM:  "-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----\n",
			CAPEM:   "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----\n",
			Serial:  "abc123",
		})
	}))
	defer srv.Close()

	certFile := filepath.Join(dir, "tls", "agent.crt")
	keyFile := filepath.Join(dir, "tls", "agent.key")
	rootCA := filepath.Join(dir, "tls", "ca.pem")

	err := EnsureCertificate(context.Background(), &conf.LeafNode{
		Tls:        &conf.LeafNode_TLS{CertFile: certFile, KeyFile: keyFile, RootCa: rootCA},
		Enrollment: &conf.Enrollment{Url: srv.URL, Token: "tok-1"},
	}, "SN-1")
	if err != nil {
		t.Fatalf("EnsureCertificate() error = %v", err)
	}

	if got.Token != "tok-1" || got.SN != "SN-1" {
		t.Fatalf("request = %+v, want token tok-1 and sn SN-1", got)
	}

	for _, path := range []string{certFile, keyFile, rootCA} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to be written: %v", path, err)
		}
	}

	// The private key must not be readable by other users.
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("private key permissions = %o, want 600", perm)
	}
}

func TestEnsureCertificate_SurfacesRefusal(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(enrollmentResponse{Error: "token expired"})
	}))
	defer srv.Close()

	err := EnsureCertificate(context.Background(), &conf.LeafNode{
		Tls:        &conf.LeafNode_TLS{CertFile: filepath.Join(dir, "a.crt"), KeyFile: filepath.Join(dir, "a.key"), RootCa: filepath.Join(dir, "ca.pem")},
		Enrollment: &conf.Enrollment{Url: srv.URL, Token: "tok"},
	}, "SN-1")
	if err == nil {
		t.Fatal("expected an error when the control plane refuses enrollment")
	}
}

func TestEnsureCertificate_RequiresToken(t *testing.T) {
	dir := t.TempDir()
	err := EnsureCertificate(context.Background(), &conf.LeafNode{
		Tls:        &conf.LeafNode_TLS{CertFile: filepath.Join(dir, "a.crt"), KeyFile: filepath.Join(dir, "a.key"), RootCa: filepath.Join(dir, "ca.pem")},
		Enrollment: &conf.Enrollment{Url: "https://example.invalid"},
	}, "SN-1")
	if err == nil {
		t.Fatal("expected an error when neither token nor token_file is configured")
	}
}

func TestEnsureCertificate_RequiresEnrollmentURL(t *testing.T) {
	dir := t.TempDir()
	err := EnsureCertificate(context.Background(), &conf.LeafNode{
		Tls: &conf.LeafNode_TLS{CertFile: filepath.Join(dir, "a.crt"), KeyFile: filepath.Join(dir, "a.key"), RootCa: filepath.Join(dir, "ca.pem")},
	}, "SN-1")
	if err == nil {
		t.Fatal("expected an error when no certificate exists and no enrollment url is set")
	}
}

func TestReadToken_PrefersFile(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	token, err := readToken(&conf.Enrollment{Token: "from-inline", TokenFile: tokenFile})
	if err != nil {
		t.Fatalf("readToken() error = %v", err)
	}
	if token != "from-file" {
		t.Fatalf("readToken() = %q, want the file contents", token)
	}
}

func TestWriteSecret_IsAtomicAndRestrictive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "secret.key")

	if err := writeSecret(path, []byte("v1")); err != nil {
		t.Fatalf("writeSecret() error = %v", err)
	}
	if err := writeSecret(path, []byte("v2")); err != nil {
		t.Fatalf("writeSecret() overwrite error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("content = %q, want v2", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permissions = %o, want 600", perm)
	}

	// No temporary files may be left behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "secret.key" {
			t.Fatalf("unexpected leftover file %q", e.Name())
		}
	}
}
