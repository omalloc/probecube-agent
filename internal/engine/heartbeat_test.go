package engine

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/omalloc/probecube-agent/internal/messaging"
	"google.golang.org/protobuf/proto"
)

type heartbeatPublisher struct {
	messaging.Publisher
	sent chan messaging.Header
}

func (p *heartbeatPublisher) HasReady() bool { return true }
func (p *heartbeatPublisher) PublishWithHeaders(_ context.Context, subject string, h messaging.Header, _ proto.Message) error {
	if subject == DialSysHeartbeatSubject {
		p.sent <- h
	}
	return nil
}
func TestHeartbeatWithoutNetworkAddresses(t *testing.T) {
	p := &heartbeatPublisher{sent: make(chan messaging.Header, 1)}
	c := &Collector{publisher: p, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); c.heartbeat(ctx) }()
	defer func() { cancel(); <-done }()
	select {
	case h := <-p.sent:
		if h.Get("ip") != "" || h.Get("priv_ip") != "" {
			t.Fatalf("unexpected addresses: %v", h)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("heartbeat blocked by missing STUN addresses")
	}
}
