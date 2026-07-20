package probers

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"github.com/omalloc/probecube-agent/internal/engine"
)

type ICMPProber struct{}

func NewICMPProber() *ICMPProber {
	return &ICMPProber{}
}

func init() {
	engine.Register(engine.ICMP, NewICMPProber())
}

func (p *ICMPProber) Probe(ctx context.Context, task *engine.Task) (*engine.Result, error) {
	start := time.Now()

	// 1. 建立 ICMP 连接
	// "udp4" 尝试非特权模式，某些环境下需要配置 net.ipv4.ping_group_range
	// "ip4:icmp" 为特权模式，需要 root 权限
	network := "udp4"
	if os.Getuid() == 0 {
		network = "ip4:icmp"
	}

	conn, err := icmp.ListenPacket(network, "0.0.0.0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w (check permissions)", network, err)
	}
	defer conn.Close()

	dest, err := net.ResolveIPAddr("ip4", task.Target)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve target: %w", err)
	}

	// 2. 构造报文
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("HELLO-PROBE"),
		},
	}
	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	// 3. 发送并等待结果
	deadline := time.Now().Add(task.Timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	var dstAddr net.Addr
	if network == "udp4" {
		dstAddr = &net.UDPAddr{IP: dest.IP}
	} else {
		dstAddr = dest
	}

	if _, err := conn.WriteTo(msgBytes, dstAddr); err != nil {
		return nil, fmt.Errorf("failed to send ICMP: %w", err)
	}

	reply := make([]byte, 1500)
	n, _, err := conn.ReadFrom(reply)
	duration := time.Since(start)

	result := &engine.Result{
		TaskID:    task.ID,
		TaskSN:    task.TaskSN,
		Proto:     task.Proto,
		Type:      task.Type,
		Duration:  duration,
		LatencyMs: float64(duration.Microseconds()) / 1000.0,
		Metrics:   make(map[string]interface{}),
	}

	if err != nil {
		result.Success = false
		result.Status = "Timeout"
		result.Err = err
		return result, nil
	}

	// 4. 解析回包
	rm, err := icmp.ParseMessage(1, reply[:n])
	if err != nil {
		result.Success = false
		result.Status = "Parse Error"
		result.Err = err
		return result, nil
	}

	if rm.Type == ipv4.ICMPTypeEchoReply {
		result.Success = true
		result.Status = "Alive"
	} else {
		result.Success = false
		result.Status = fmt.Sprintf("Unexpected Type: %v", rm.Type)
	}

	return result, nil
}
