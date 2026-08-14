package probers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/omalloc/probecube-agent/internal/engine"
)

func TestICMPProber_Probe_InvalidTarget(t *testing.T) {
	p := NewICMPProber()
	if _, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, Proto: engine.ICMP, Target: "999.999.999.999", Timeout: time.Second,
	}); err == nil || !strings.Contains(err.Error(), "failed to resolve target") {
		t.Fatalf("Probe() error = %v, want resolve failure", err)
	}
}

func TestICMPProber_Probe_Localhost(t *testing.T) {
	p := NewICMPProber()
	result, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, TaskSN: "icmp-local", Proto: engine.ICMP,
		Target: "127.0.0.1", Timeout: 2 * time.Second,
	})
	if err != nil {
		// 非特权环境且未配置 net.ipv4.ping_group_range 时无法建立 ICMP socket。
		if strings.Contains(err.Error(), "failed to listen") {
			t.Skipf("icmp socket unavailable: %v", err)
		}
		t.Fatalf("Probe() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Probe() success = false, want true (status=%s, err=%v)", result.Status, result.Err)
	}
	if result.LatencyMs <= 0 {
		t.Fatalf("Probe() latency = %f, want > 0", result.LatencyMs)
	}
	if rtt, ok := result.Metrics["rtt_ms"].(float64); !ok || rtt <= 0 {
		t.Fatalf("Metrics[rtt_ms] = %v, want > 0", result.Metrics["rtt_ms"])
	}
}
