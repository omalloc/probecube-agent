package probers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/omalloc/probecube-agent/internal/engine"
)

func TestBrowserMetrics_FromScriptOutput(t *testing.T) {
	payload := []byte(`{"fcp_ms":12.3,"lcp_ms":45.6,"cls":0.01,"dcl_ms":78.9,"load_ms":100.2,"title":"probe","url":"http://example.com/"}`)

	var m browserMetrics
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("unmarshal script output: %v", err)
	}
	metrics := m.toMetrics()
	if metrics["fcp_ms"] != 12.3 || metrics["lcp_ms"] != 45.6 || metrics["cls"] != 0.01 {
		t.Fatalf("metrics = %+v", metrics)
	}
	if metrics["title"] != "probe" || metrics["final_url"] != "http://example.com/" {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestBrowserProber_Probe_MissingExecutable(t *testing.T) {
	p := &BrowserProber{
		allocatorOpts: []chromedp.ExecAllocatorOption{
			chromedp.ExecPath("/nonexistent/chrome-probecube"),
		},
	}
	_, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, Proto: engine.Browser, Target: "http://127.0.0.1/", Timeout: 2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "browser executable not found") {
		t.Fatalf("Probe() error = %v, want browser executable not found", err)
	}
}

func TestBrowserProber_Probe_InvalidTarget(t *testing.T) {
	if browserExecutable() == "" {
		t.Skip("chrome not installed")
	}
	p := NewBrowserProber()
	result, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, Proto: engine.Browser, Target: "http://nonexistent.invalid/", Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Probe() error = %v, want failure result", err)
	}
	if result.Success {
		t.Fatalf("Probe() success = true, want false for unresolvable host")
	}
}

func TestBrowserProber_Probe_LocalPage(t *testing.T) {
	if browserExecutable() == "" {
		t.Skip("chrome not installed")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>probe-ok</title></head><body><h1>hello</h1></body></html>`))
	}))
	defer srv.Close()

	p := NewBrowserProber()
	result, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, TaskSN: "browser-local", Proto: engine.Browser,
		Target: srv.URL, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Probe() success = false, want true (status=%s, err=%v)", result.Status, result.Err)
	}
	if got := result.Metrics["http_status"]; got != int64(200) {
		t.Fatalf("Metrics[http_status] = %v, want 200", got)
	}
	if got := result.Metrics["title"]; got != "probe-ok" {
		t.Fatalf("Metrics[title] = %v, want probe-ok", got)
	}
	if loadMs, ok := result.Metrics["load_ms"].(float64); !ok || loadMs <= 0 {
		t.Fatalf("Metrics[load_ms] = %v, want > 0", result.Metrics["load_ms"])
	}
	if fcpMs, ok := result.Metrics["fcp_ms"].(float64); !ok || fcpMs < 0 {
		t.Fatalf("Metrics[fcp_ms] = %v", result.Metrics["fcp_ms"])
	}
}

func TestBrowserProber_Probe_TimingOutPage(t *testing.T) {
	if browserExecutable() == "" {
		t.Skip("chrome not installed")
	}

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 发出部分响应体后挂起,永不触发 load 事件,等待探测超时。
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`<!doctype html><html><body>`))
		if flusher != nil {
			flusher.Flush()
		}
		<-release
	}))
	defer close(release) // 先释放 handler,再关闭 server(LIFO)
	defer srv.Close()

	p := NewBrowserProber()
	result, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, Proto: engine.Browser, Target: srv.URL, Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Probe() error = %v, want failure result", err)
	}
	if result.Success {
		t.Fatalf("Probe() success = true, want false on timeout")
	}
	if result.Err == nil {
		t.Fatalf("Probe() err = nil, want timeout error")
	}
}
