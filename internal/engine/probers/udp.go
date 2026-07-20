package probers

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/omalloc/probecube-agent/internal/engine"
)

type UDPProber struct{}

func NewUDPProber() *UDPProber {
	return &UDPProber{}
}

func init() {
	engine.Register(engine.UDP, NewUDPProber())
}

func (p *UDPProber) Probe(ctx context.Context, task *engine.Task) (*engine.Result, error) {
	start := time.Now()

	deadline := task.Timeout

	conn, err := net.DialTimeout("udp", task.Target, deadline)
	if err != nil {
		return nil, fmt.Errorf("failed to dial udp: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(deadline)); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	payload := task.Payload
	if len(payload) == 0 {
		payload = []byte("PING")
	}

	sent, err := conn.Write(payload)
	if err != nil {
		return &engine.Result{
			TaskID:    task.ID,
			TaskSN:    task.TaskSN,
			Type:      task.Type,
			Proto:     task.Proto,
			Success:   false,
			Duration:  time.Since(start),
			LatencyMs: float64(time.Since(start).Microseconds()) / 1000.0,
			Status:    "Send Failed",
			Err:       err,
		}, nil
	}

	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	duration := time.Since(start)

	result := &engine.Result{
		TaskID:    task.ID,
		TaskSN:    task.TaskSN,
		Type:      task.Type,
		Proto:     task.Proto,
		Duration:  duration,
		LatencyMs: float64(duration.Microseconds()) / 1000.0,
		Metrics:   make(map[string]interface{}),
	}

	if err != nil {
		// Timeout or ICMP port unreachable — port may be filtered or closed.
		// UDP is fire-and-forget, so timeout doesn't necessarily mean failure.
		if nErr, ok := err.(net.Error); ok && nErr.Timeout() {
			result.Success = true
			result.Status = "Sent (no response)"
			result.Metrics["bytes_sent"] = sent
			result.Metrics["bytes_received"] = 0
			return result, nil
		}

		result.Success = false
		result.Status = "Read Failed"
		result.Err = err
		result.Metrics["bytes_sent"] = sent
		return result, nil
	}

	result.Success = true
	result.Status = "Responded"
	result.Metrics["bytes_sent"] = sent
	result.Metrics["bytes_received"] = n
	result.Metrics["response"] = string(buf[:min(n, 64)])

	return result, nil
}
