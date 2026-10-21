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
	v1 "github.com/omalloc/probecube-api/message/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

const DialSysHeartbeatSubject = "dialsys.heartbeat"
const DialSysProbeTaskSubject = "dialsys.probe.task"
const DialSysProbeResultsSubject = "dialsys.probe.results"
const DialSysNodeMetadataSubject = "dialsys.node.metadata"

var hostname, _ = os.Hostname()

var _ transport.Server = (*Collector)(nil)

type Collector struct {
	log        *slog.Logger
	publisher  messaging.Publisher
	subscriber messaging.Subscriber
	engine     ProbeEngineScheduler
	nodeSN     string
	netAddrs   atomic.Value
	metadata   atomic.Pointer[v1.NodeMetadata]
}

func NewCollector(client *messaging.Client, publisher messaging.Publisher, subscriber messaging.Subscriber, engine ProbeEngineScheduler, logger *slog.Logger) *Collector {
	return &Collector{
		log:        logger,
		publisher:  publisher,
		subscriber: subscriber,
		engine:     engine,
		nodeSN:     client.SN(),
		netAddrs:   atomic.Value{},
		metadata:   atomic.Pointer[v1.NodeMetadata]{},
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

	go c.requestNodeMetadata(ctx)

	// Fail fast when the task subscription cannot be established: an agent that
	// silently runs without it looks healthy while receiving no work.
	if err := c.subscribes(ctx); err != nil {
		return err
	}

	return nil
}

// Stop implements [transport.Server].
func (c *Collector) Stop(context.Context) error {
	return nil
}

func (c *Collector) subscribes(ctx context.Context) error {
	handler := func(sctx context.Context, _ messaging.Header, payload []byte) error {
		var message v1.TaskMessage
		if err := proto.Unmarshal(payload, &message); err != nil {
			c.log.Info("unmarshal task failed", "err", err)
			return err
		}

		c.log.Info("recevice new task",
			"task_sn", message.TaskSn,
			"protocol", message.Protocol,
			"run_id", message.RunId,
			"target", message.Target)

		c.engine.Submit(sctx, taskFromProto(&message))
		return nil
	}

	// Only the node-specific subject is subscribed. Broadcasting every task to
	// every agent is both a cross-node delivery risk and incompatible with the
	// subject-level permissions the auth callout service grants, which allow
	// subscribing to dialsys.probe.task.<this node> and nothing wider.
	if err := c.subscriber.Subscribe(ctx, DialSysProbeTaskSubject+"."+c.nodeSN, handler); err != nil {
		return fmt.Errorf("subscribe probe task subject: %w", err)
	}
	return nil
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
		case result, ok := <-c.engine.Results():
			if !ok {
				c.log.ErrorContext(ctx, "task channel closed.")
				return
			}
			if result.Success {
				c.log.DebugContext(ctx, "Probe task success", "task_id", result.TaskID)
			} else {
				c.log.DebugContext(ctx, "Probe task failed", "task_id", result.TaskID)
			}

			// task result.
			payload := toTaskResultProto(result, c.nodeSN)

			c.log.Debug("report task result", "payload", payload)
			if err := c.publisher.Publish(ctx, DialSysProbeResultsSubject, payload); err != nil {
				c.log.ErrorContext(ctx, "report task result failed", "err", err)
			}
		}
	}
}

func (c *Collector) requestNodeMetadata(ctx context.Context) {

	load := func() {
		metadata := &v1.NodeMetadata{}

		c.publisher.RequestWithHeaders(context.Background(), DialSysNodeMetadataSubject, nil, nil, metadata)

		c.log.DebugContext(ctx, "[nats] request", "subject", DialSysNodeMetadataSubject, "reply", metadata)

		if metadata.Id > 0 {
			c.metadata.Store(metadata)
		}
	}

	load()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			load()
		}
	}
}

func toTaskResultProto(r *Result, nodeSN string) proto.Message {
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
		NodeId:     nodeSN, // 这里直接取本机 SN
		RunId:      r.RunID,
		Protocol:   protocol,
		Target:     r.Target,
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
