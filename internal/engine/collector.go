package engine

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/go-kratos/kratos/v3/transport"
	"github.com/omalloc/probecube-agent/internal/messaging"
	"github.com/omalloc/probecube-agent/pkg/netinfo"
	v1 "github.com/omalloc/probecube-console/api/message/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

const DialSysHeartbeatSubject = "dialsys.heartbeat"
const DialSysProbingTaskSubject = "dialsys.probing.task"
const DialSysProbingResultsSubject = "dialsys.probing.results"

var hostname, _ = os.Hostname()

var _ transport.Server = (*Collector)(nil)

type Collector struct {
	log        *slog.Logger
	publisher  messaging.Publisher
	subscriber messaging.Subscriber
	es         EngineScheduler
	netAddrs   atomic.Value
}

func NewCollector(publisher messaging.Publisher, subscriber messaging.Subscriber, es EngineScheduler, logger *slog.Logger) *Collector {
	return &Collector{
		log:        logger,
		publisher:  publisher,
		subscriber: subscriber,
		es:         es,
		netAddrs:   atomic.Value{},
	}
}

// Start implements [transport.Server].
func (c *Collector) Start(ctx context.Context) error {
	interval := time.Millisecond * 200

	for {
		if c.publisher.HasReady() {
			break
		}

		c.log.WarnContext(ctx, "Waiting for publisher and subscriber to be ready")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}

	go c.netdial(ctx)

	go c.received(ctx)

	go c.heartbeat(ctx)

	c.subscribes(ctx)

	return nil
}

// Stop implements [transport.Server].
func (c *Collector) Stop(context.Context) error {
	return nil
}

func (c *Collector) subscribes(ctx context.Context) {
	// 订阅下发的探测任务
	c.subscriber.Subscribe(ctx, DialSysProbingTaskSubject, func(sctx context.Context, header messaging.Header, payload []byte) error {
		return nil
	})
}

func (c *Collector) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			netAddrs, ok := c.netAddrs.Load().(netinfo.Addrs)
			if !ok {
				continue
			}

			h := make(messaging.Header)
			h.Set("host", hostname)
			h.Set("ip", netAddrs.PublicIP)
			h.Set("priv_ip", netAddrs.LocalIP)

			_ = c.publisher.PublishWithHeaders(ctx, DialSysHeartbeatSubject, h, nil)
		}
	}
}

func (c *Collector) netdial(ctx context.Context) {
	load := func() {
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		netAddrs, err := netinfo.Discover(sctx)
		if err != nil {
			c.log.WarnContext(ctx, "Failed to refresh network addresses", "err", err)
			return
		}

		c.log.InfoContext(ctx, "Network addresses", "public_ip", netAddrs.PublicIP, "local_ip", netAddrs.LocalIP)

		c.netAddrs.Store(netAddrs)
	}

	load()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 刷新 IP 地址逻辑
			load()
		}
	}
}

func (c *Collector) received(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case result, ok := <-c.es.Results():
			if !ok {
				c.log.ErrorContext(ctx, "task channel closed.")
				return
			}

			if result.Success {
				c.log.InfoContext(ctx, "Probe task success", "task_id", result.TaskID)
			} else {
				c.log.InfoContext(ctx, "Probe task failed", "task_id", result.TaskID)
			}

			payload := toProto(result)
			if err := c.publisher.Publish(ctx, DialSysProbingResultsSubject, payload); err != nil {
				c.log.ErrorContext(ctx, "report task result failed", "err", err)
			}
		}
	}
}

func toProto(r *Result) proto.Message {
	return &v1.TaskResult{
		TaskId: r.TaskID,
		TaskSn: r.TaskSN,
		// Protocol: v1.Protocol(v1.Protocol_value[r.Proto]),
		Success:    r.Success,
		Duration:   durationpb.New(r.Duration),
		LatencyMs:  int64(r.LatencyMs),
		StatusInfo: r.Status,
		// Metadata:   r.Metrics,
	}
}
