package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/transport"
	"github.com/omalloc/probecube-agent/internal/messaging"
	v1 "github.com/omalloc/probecube-console/api/message/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

const DialSysHeartbeatSubject = "dialsys.heartbeat"
const DialSysProbingTaskSubject = "dialsys.probing.task"
const DialSysProbingResultsSubject = "dialsys.probing.results"

var hostname, _ = os.Hostname()

type GeoInfo struct {
	Query         string `json:"query"`
	ISP           string `json:"isp"`
	Country       string `json:"country"`
	CountryName   string `json:"countryName"`
	Region        string `json:"region"`
	City          string `json:"city"`
	ZipCode       string `json:"zipCode"`
	ContinentCode string `json:"continentCode"`
	ContinentName string `json:"continentName"`
}

var _ transport.Server = (*Collector)(nil)

type Collector struct {
	log        *slog.Logger
	publisher  messaging.Publisher
	subscriber messaging.Subscriber
	es         EngineScheduler
	httpClient *http.Client
	geoInfo    atomic.Value
}

func NewCollector(publisher messaging.Publisher, subscriber messaging.Subscriber, es EngineScheduler, logger *slog.Logger) *Collector {
	log.Info("xxxxxxxxxx")

	return &Collector{
		log:        logger,
		publisher:  publisher,
		subscriber: subscriber,
		es:         es,
		httpClient: &http.Client{
			Transport: &http.Transport{},
		},
		geoInfo: atomic.Value{},
	}
}

// Start implements [transport.Server].
func (c *Collector) Start(ctx context.Context) error {
	interval := time.Millisecond * 200

	c.log.Info("start collector.")

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

	go c.tickGeoInfo(ctx)

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
			geoInfo, ok := c.geoInfo.Load().(GeoInfo)
			if !ok {
				continue
			}

			h := make(messaging.Header)
			h.Set("host", hostname)
			h.Set("ip", geoInfo.Query)
			h.Set("priv_ip", "")

			c.log.InfoContext(ctx, "heartbeat", "header", h)

			_ = c.publisher.PublishWithHeaders(ctx, DialSysHeartbeatSubject, h, nil)
		}
	}
}

func (c *Collector) tickGeoInfo(ctx context.Context) {
	load := func() {
		reply, err := c.httpClient.Get("https://ip.konoaho.com/api/v1/ipquery")
		if err != nil {
			return
		}

		var geoInfo GeoInfo
		if err := json.NewDecoder(reply.Body).Decode(&geoInfo); err != nil {
			c.log.WarnContext(ctx, "Failed to decode public IP response failed", "err", err)
			return
		}

		c.log.InfoContext(ctx, "tickGeoInfo", "geoInfo", geoInfo)

		c.geoInfo.Store(geoInfo)
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
