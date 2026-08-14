package probers

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omalloc/probecube-agent/internal/engine"
)

func newTestHTTPProber(t *testing.T, followRedirects bool) *HTTPProber {
	t.Helper()
	transport := &http.Transport{
		DisableKeepAlives: true,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: transport}
	if !followRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return &HTTPProber{client: client}
}

func TestHTTPProber_Probe_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantOK     bool
	}{
		{"ok", http.StatusOK, true},
		{"not_found", http.StatusNotFound, false},
		{"server_error", http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			p := newTestHTTPProber(t, true)
			result, err := p.Probe(context.Background(), &engine.Task{
				ID: 1, TaskSN: "http-"+tt.name, Proto: engine.HTTP,
				Target: srv.URL, Timeout: 5 * time.Second,
			})
			if err != nil {
				t.Fatalf("Probe() error = %v", err)
			}
			if result.Success != tt.wantOK {
				t.Fatalf("Probe() success = %v, want %v", result.Success, tt.wantOK)
			}
			if got := result.Metrics["http_code"]; got != tt.statusCode {
				t.Fatalf("Metrics[http_code] = %v, want %d", got, tt.statusCode)
			}
			if result.Status != fmt.Sprintf("%d %s", tt.statusCode, http.StatusText(tt.statusCode)) {
				t.Fatalf("Probe() status = %q", result.Status)
			}
		})
	}
}

func TestHTTPProber_Probe_RedirectNotFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/final")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	p := newTestHTTPProber(t, false)
	result, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, TaskSN: "http-redirect", Proto: engine.HTTP,
		Target: srv.URL, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	// 3xx 视为可达,且未跟随跳转。
	if !result.Success {
		t.Fatalf("Probe() success = false, want true")
	}
	if got := result.Metrics["http_code"]; got != http.StatusFound {
		t.Fatalf("Metrics[http_code] = %v, want %d", got, http.StatusFound)
	}
}

func TestHTTPProber_Probe_TLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestHTTPProber(t, true)
	result, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, TaskSN: "https-ok", Proto: engine.HTTPS,
		Target: srv.URL, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Probe() success = false, want true")
	}
	if _, ok := result.Metrics["ssl_expiry"]; !ok {
		t.Fatalf("Metrics[ssl_expiry] missing: %+v", result.Metrics)
	}
	if days, ok := result.Metrics["ssl_days_left"].(int); !ok || days < 0 {
		t.Fatalf("Metrics[ssl_days_left] = %v", result.Metrics["ssl_days_left"])
	}
}

func TestHTTPProber_Probe_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestHTTPProber(t, true)
	result, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, TaskSN: "http-timeout", Proto: engine.HTTP,
		Target: srv.URL, Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Probe() error = %v, want result with failure", err)
	}
	if result.Success {
		t.Fatalf("Probe() success = true, want false")
	}
	if result.Err == nil {
		t.Fatalf("Probe() err = nil, want timeout error")
	}
	if result.Status != "Error" {
		t.Fatalf("Probe() status = %q, want Error", result.Status)
	}
}

func TestHTTPProber_Probe_InvalidTarget(t *testing.T) {
	p := newTestHTTPProber(t, true)
	if _, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, Proto: engine.HTTP, Target: "://bad-url", Timeout: time.Second,
	}); err == nil {
		t.Fatalf("Probe() error = nil, want invalid target error")
	}
}
