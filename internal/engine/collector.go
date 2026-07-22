package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	nodeSN     string
	netAddrs   atomic.Value
}

func NewCollector(client *messaging.Client, publisher messaging.Publisher, subscriber messaging.Subscriber, es EngineScheduler, logger *slog.Logger) *Collector {
	return &Collector{
		log:        logger,
		publisher:  publisher,
		subscriber: subscriber,
		es:         es,
		nodeSN:     client.SN(),
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
	handler := func(sctx context.Context, _ messaging.Header, payload []byte) error {
		var message v1.TaskMessage
		if err := proto.Unmarshal(payload, &message); err != nil {
			return err
		}
		c.es.Submit(sctx, taskFromProto(&message))
		return nil
	}

	// Keep the shared subject for backwards-compatible broadcast tasks and add
	// the node-specific subject used by NodeSelector dispatch.
	c.subscriber.Subscribe(ctx, DialSysProbingTaskSubject, handler)
	c.subscriber.Subscribe(ctx, DialSysProbingTaskSubject+"."+c.nodeSN, handler)
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

			payload := toTaskResultProto(result)
			if err := c.publisher.Publish(ctx, DialSysProbingResultsSubject, payload); err != nil {
				c.log.ErrorContext(ctx, "report task result failed", "err", err)
			}
		}
	}
}

func toTaskResultProto(r *Result) proto.Message {
	protocol := v1.Protocol_PROTOCOL_UNSPECIFIED
	if value, ok := v1.Protocol_value["PROTOCOL_"+strings.ToUpper(string(r.Proto))]; ok {
		protocol = v1.Protocol(value)
	}
	metadata := make(map[string]string, len(r.Metrics))
	for key, value := range r.Metrics {
		metadata[key] = fmt.Sprint(value)
	}
	var resultErrors map[string]string
	if r.Err != nil {
		resultErrors = map[string]string{"message": r.Err.Error()}
	}
	return &v1.TaskResult{
		TaskId:     r.TaskID,
		TaskSn:     r.TaskSN,
		RunId:      r.RunID,
		Protocol:   protocol,
		Success:    r.Success,
		Duration:   durationpb.New(r.Duration),
		LatencyMs:  int64(r.LatencyMs),
		StatusInfo: r.Status,
		Metadata:   metadata,
		Errors:     resultErrors,
	}
}

func taskFromProto(message *v1.TaskMessage) *Task {
	timeout := time.Duration(0)
	if message.Timeout != nil {
		timeout = message.Timeout.AsDuration()
	}
	return &Task{
		ID:       message.TaskId,
		RunID:    message.RunId,
		TaskSN:   message.TaskSn,
		Proto:    Protocol(strings.TrimPrefix(message.Protocol.String(), "PROTOCOL_")),
		Type:     "once",
		Target:   message.Target,
		Timeout:  timeout,
		Payload:  message.Payload,
		Metadata: message.Metadata,
	}
}
