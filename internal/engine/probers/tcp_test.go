package probers

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/omalloc/probecube-agent/internal/engine"
)

func TestTCPHeader_Marshal(t *testing.T) {
	h := &TCPHeader{
		SrcPort: 0x1234, DstPort: 0x0050, SeqNum: 0xDEADBEEF, AckNum: 0x01020304,
		DataOff: 5, Flags: 0x02, Window: 0xFAF0, Checksum: 0xABCD, Urgent: 0x0011,
	}
	buf := h.Marshal()
	if len(buf) != 20 {
		t.Fatalf("Marshal() len = %d, want 20", len(buf))
	}
	checks := []struct {
		name string
		got  uint32
		want uint32
	}{
		{"src port", uint32(binary.BigEndian.Uint16(buf[0:2])), 0x1234},
		{"dst port", uint32(binary.BigEndian.Uint16(buf[2:4])), 0x0050},
		{"seq", binary.BigEndian.Uint32(buf[4:8]), 0xDEADBEEF},
		{"ack", binary.BigEndian.Uint32(buf[8:12]), 0x01020304},
		{"window", uint32(binary.BigEndian.Uint16(buf[14:16])), 0xFAF0},
		{"checksum", uint32(binary.BigEndian.Uint16(buf[16:18])), 0xABCD},
		{"urgent", uint32(binary.BigEndian.Uint16(buf[18:20])), 0x0011},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Fatalf("%s = %#x, want %#x", c.name, c.got, c.want)
		}
	}
	if buf[12] != 0x50 { // DataOff 5 << 4
		t.Fatalf("byte[12] = %#x, want 0x50", buf[12])
	}
	if buf[13] != 0x02 {
		t.Fatalf("byte[13] (flags) = %#x, want 0x02", buf[13])
	}
}

func TestTCPHeader_CalcChecksum(t *testing.T) {
	tests := []struct {
		name string
		src  string
		dst  string
		data []byte
	}{
		{"no_payload", "192.0.2.1", "198.51.100.2", nil},
		{"with_payload", "10.0.0.1", "10.0.0.2", []byte("GET / HTTP/1.1\r\n")},
		{"odd_length_payload", "172.16.0.1", "172.16.0.2", []byte("abc")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := net.ParseIP(tt.src).To4()
			dst := net.ParseIP(tt.dst).To4()
			h := &TCPHeader{SrcPort: 40000, DstPort: 80, SeqNum: 1, DataOff: 5, Flags: 0x02, Window: 64240}
			h.Checksum = h.CalcChecksum(src, dst, tt.data)

			// RFC 1071 性质验证:将已含 checksum 的报文(伪头部 + TCP 段)按 16 位
			// 求和并折叠进位,结果必须为 0xFFFF。
			sum := uint32(0)
			add := func(b []byte) {
				for i := 0; i < len(b); i += 2 {
					if i+1 < len(b) {
						sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
					} else {
						sum += uint32(b[i]) << 8
					}
				}
			}
			pseudo := make([]byte, 12)
			copy(pseudo[0:4], src)
			copy(pseudo[4:8], dst)
			pseudo[9] = 6 // IPPROTO_TCP
			binary.BigEndian.PutUint16(pseudo[10:12], uint16(20+len(tt.data)))
			add(pseudo)
			add(h.Marshal())
			add(tt.data)
			for sum > 0xffff {
				sum = (sum & 0xffff) + (sum >> 16)
			}
			if sum != 0xffff {
				t.Fatalf("checksum verification sum = %#x, want 0xffff", sum)
			}
		})
	}
}

func TestTCPProber_GetSourceIP_Loopback(t *testing.T) {
	p := NewTCPProber()
	ip := p.getSourceIP(net.ParseIP("127.0.0.1"))
	if !ip.IsLoopback() {
		t.Fatalf("getSourceIP(127.0.0.1) = %v, want loopback", ip)
	}
}

func TestTCPProber_Probe_InvalidTarget(t *testing.T) {
	p := NewTCPProber()
	if _, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, Proto: engine.TCP, Target: "no-port-here", Timeout: time.Second,
	}); err == nil || !strings.Contains(err.Error(), "invalid target format") {
		t.Fatalf("Probe() error = %v, want invalid target format", err)
	}
}

func TestTCPProber_Probe_Localhost(t *testing.T) {
	p := NewTCPProber()
	result, err := p.Probe(context.Background(), &engine.Task{
		ID: 1, TaskSN: "tcp-local", Proto: engine.TCP,
		Target: "127.0.0.1:9", Timeout: 2 * time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "failed to create raw socket") {
			t.Skipf("raw socket unavailable: %v", err)
		}
		t.Fatalf("Probe() error = %v", err)
	}
	// 对 loopback 关闭端口,预期收到 RST;某些环境可能返回 SYN-ACK。
	if !result.Success {
		t.Fatalf("Probe() success = false, want true (status=%s, err=%v)", result.Status, result.Err)
	}
	if !strings.HasPrefix(result.Status, "Alive") {
		t.Fatalf("Probe() status = %q, want Alive prefix", result.Status)
	}
}
