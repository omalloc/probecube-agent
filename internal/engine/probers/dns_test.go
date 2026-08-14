package probers

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/omalloc/probecube-agent/internal/engine"
)

func startDNSServer(t *testing.T, handler dns.HandlerFunc) string {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}

	srv := &dns.Server{PacketConn: pc, Handler: handler}
	go func() {
		_ = srv.ActivateAndServe()
	}()
	t.Cleanup(func() { _ = srv.Shutdown() })

	return pc.LocalAddr().String()
}

func dnsTask(t *testing.T, server string, timeout time.Duration) *engine.Task {
	t.Helper()
	return &engine.Task{
		ID: 1, TaskSN: "dns-test", Proto: engine.DNS,
		Target: "example.test.", Timeout: timeout,
		Metadata: map[string]string{"server": server},
	}
}

func TestDNSProber_Probe_Success(t *testing.T) {
	server := startDNSServer(t, func(w dns.ResponseWriter, r *dns.Msg) {
		reply := new(dns.Msg)
		reply.SetReply(r)
		reply.Answer = append(reply.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   net.ParseIP("192.0.2.1"),
		})
		_ = w.WriteMsg(reply)
	})

	p := NewDNSProber()
	result, err := p.Probe(context.Background(), dnsTask(t, server, 2*time.Second))
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Probe() success = false, want true (status=%s, err=%v)", result.Status, result.Err)
	}
	if result.Status != "Success" {
		t.Fatalf("Probe() status = %q, want Success", result.Status)
	}
	if got := result.Metrics["rcode"]; got != "NOERROR" {
		t.Fatalf("Metrics[rcode] = %v, want NOERROR", got)
	}
	answers, ok := result.Metrics["answers"].([]string)
	if !ok || len(answers) != 1 || answers[0] != "192.0.2.1" {
		t.Fatalf("Metrics[answers] = %v", result.Metrics["answers"])
	}
	if rtt, ok := result.Metrics["rtt_ms"].(float64); !ok || rtt < 0 {
		t.Fatalf("Metrics[rtt_ms] = %v", result.Metrics["rtt_ms"])
	}
}

func TestDNSProber_Probe_NXDOMAIN(t *testing.T) {
	server := startDNSServer(t, func(w dns.ResponseWriter, r *dns.Msg) {
		reply := new(dns.Msg)
		reply.SetReply(r)
		reply.Rcode = dns.RcodeNameError
		_ = w.WriteMsg(reply)
	})

	p := NewDNSProber()
	result, err := p.Probe(context.Background(), dnsTask(t, server, 2*time.Second))
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if result.Success {
		t.Fatalf("Probe() success = true, want false for NXDOMAIN")
	}
	if result.Status != "NXDOMAIN" {
		t.Fatalf("Probe() status = %q, want NXDOMAIN", result.Status)
	}
}

func TestDNSProber_Probe_Timeout(t *testing.T) {
	server := startDNSServer(t, func(w dns.ResponseWriter, r *dns.Msg) {
		time.Sleep(500 * time.Millisecond) // 不应答,模拟黑洞
	})

	p := NewDNSProber()
	result, err := p.Probe(context.Background(), dnsTask(t, server, 100*time.Millisecond))
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if result.Success {
		t.Fatalf("Probe() success = true, want false on timeout")
	}
	if result.Err == nil {
		t.Fatalf("Probe() err = nil, want timeout error")
	}
	if result.Status != "Query Failed" {
		t.Fatalf("Probe() status = %q, want Query Failed", result.Status)
	}
}
