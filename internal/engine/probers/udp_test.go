package probers

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/omalloc/probecube-agent/internal/engine"
)

func startUDPEchoServer(t *testing.T) string {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = conn.WriteTo(buf[:n], addr)
		}
	}()

	return conn.LocalAddr().String()
}

func udpTask(t *testing.T, target string, payload []byte, timeout time.Duration) *engine.Task {
	t.Helper()
	return &engine.Task{
		ID: 1, TaskSN: "udp-test", Proto: engine.UDP,
		Target: target, Payload: payload, Timeout: timeout,
	}
}

func TestUDPProber_Probe_Responded(t *testing.T) {
	server := startUDPEchoServer(t)

	p := NewUDPProber()
	result, err := p.Probe(context.Background(), udpTask(t, server, []byte("HELLO"), 2*time.Second))
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Probe() success = false, want true (status=%s, err=%v)", result.Status, result.Err)
	}
	if result.Status != "Responded" {
		t.Fatalf("Probe() status = %q, want Responded", result.Status)
	}
	if got := result.Metrics["bytes_sent"]; got != 5 {
		t.Fatalf("Metrics[bytes_sent] = %v, want 5", got)
	}
	if got := result.Metrics["bytes_received"]; got != 5 {
		t.Fatalf("Metrics[bytes_received] = %v, want 5", got)
	}
	if got := result.Metrics["response"]; !strings.Contains(got.(string), "HELLO") {
		t.Fatalf("Metrics[response] = %v, want echo of HELLO", got)
	}
}

func TestUDPProber_Probe_DefaultPayload(t *testing.T) {
	server := startUDPEchoServer(t)

	p := NewUDPProber()
	result, err := p.Probe(context.Background(), udpTask(t, server, nil, 2*time.Second))
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Probe() success = false, want true (status=%s)", result.Status)
	}
	if got := result.Metrics["bytes_sent"]; got != len("PING") {
		t.Fatalf("Metrics[bytes_sent] = %v, want %d", got, len("PING"))
	}
}

func TestUDPProber_Probe_SilentServer(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer conn.Close()

	p := NewUDPProber()
	result, err := p.Probe(context.Background(), udpTask(t, conn.LocalAddr().String(), []byte("PING"), 150*time.Millisecond))
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	// UDP 是发后即忘:无响应视为已发出,而非失败。
	if !result.Success {
		t.Fatalf("Probe() success = false, want true (status=%s)", result.Status)
	}
	if result.Status != "Sent (no response)" {
		t.Fatalf("Probe() status = %q, want Sent (no response)", result.Status)
	}
	if got := result.Metrics["bytes_received"]; got != 0 {
		t.Fatalf("Metrics[bytes_received] = %v, want 0", got)
	}
}
