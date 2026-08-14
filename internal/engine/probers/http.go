package probers

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/omalloc/probecube-agent/internal/engine"
)

type HTTPProber struct {
	client *http.Client
}

func init() {
	engine.Register(engine.HTTP, NewHTTPProber())
	engine.Register(engine.HTTPS, NewHTTPProber())
}

func NewHTTPProber() *HTTPProber {
	transport := &http.Transport{
		DisableKeepAlives: true, // 模拟首访，不复用连接
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
	}
	return &HTTPProber{
		client: &http.Client{
			Transport: transport,
		},
	}
}

func (p *HTTPProber) Probe(ctx context.Context, task *engine.Task) (*engine.Result, error) {
	start := time.Now()
	var (
		dnsStart, connectStart, tlsStart             time.Time
		dnsDone, connectDone, tlsDone, firstByteDone time.Duration
	)

	trace := &httptrace.ClientTrace{
		DNSStart:             func(_ httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:              func(_ httptrace.DNSDoneInfo) { dnsDone = time.Since(dnsStart) },
		ConnectStart:         func(_, _ string) { connectStart = time.Now() },
		ConnectDone:          func(_, _ string, _ error) { connectDone = time.Since(connectStart) },
		TLSHandshakeStart:    func() { tlsStart = time.Now() },
		TLSHandshakeDone:     func(_ tls.ConnectionState, _ error) { tlsDone = time.Since(tlsStart) },
		GotFirstResponseByte: func() { firstByteDone = time.Since(start) },
	}

	reqCtx, cancel := context.WithTimeoutCause(ctx, task.Timeout, fmt.Errorf("context deadline exceeded: %v", task.Timeout))
	defer cancel()

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(reqCtx, trace), "GET", task.Target, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	duration := time.Since(start)

	result := &engine.Result{
		TaskID:    task.ID,
		RunID:     task.RunID,
		TaskSN:    task.TaskSN,
		Proto:     task.Proto,
		Type:      task.Type,
		Duration:  duration,
		LatencyMs: float64(duration.Microseconds()) / 1000.0,
		Metrics:   make(map[string]interface{}),
	}

	if err != nil {
		result.Success = false
		result.Status = "Error"
		result.Err = err
		return result, nil
	}
	defer resp.Body.Close()

	result.Success = (resp.StatusCode >= 200 && resp.StatusCode < 400)
	result.Status = resp.Status
	result.Metrics["http_code"] = resp.StatusCode
	result.Metrics["dns_time_ms"] = dnsDone.Seconds() * 1000
	result.Metrics["connect_time_ms"] = connectDone.Seconds() * 1000
	result.Metrics["tls_handshake_ms"] = tlsDone.Seconds() * 1000
	result.Metrics["first_byte_ms"] = firstByteDone.Seconds() * 1000

	// 提取证书过期时间 (针对 HTTPS)
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		cert := resp.TLS.PeerCertificates[0]
		result.Metrics["ssl_expiry"] = cert.NotAfter.Format(time.RFC3339)
		result.Metrics["ssl_days_left"] = int(time.Until(cert.NotAfter).Hours() / 24)
	}

	return result, nil
}
