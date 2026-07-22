package engine

import (
	"context"
	"time"
)

// Protocol 探测协议类型
type Protocol string

const (
	ICMP    Protocol = "ICMP"
	TCP     Protocol = "TCP"
	UDP     Protocol = "UDP"
	HTTP    Protocol = "HTTP"
	HTTPS   Protocol = "HTTPS"
	DNS     Protocol = "DNS"
	Browser Protocol = "Browser"
)

// Task 探测任务定义
type Task struct {
	ID       int64             `json:"id"`
	RunID    int64             `json:"run_id"`
	TaskSN   string            `json:"task_sn"`
	Proto    Protocol          `json:"proto"`
	Type     string            `json:"type"`
	Target   string            `json:"target"`
	Timeout  time.Duration     `json:"timeout"`
	Payload  []byte            `json:"payload,omitempty"`  // 特殊载荷
	Metadata map[string]string `json:"metadata,omitempty"` // 额外配置：如源 IP, 关键词, 证书校验等
}

// Result 探测结果定义
type Result struct {
	TaskID    int64                  `json:"task_id"`
	RunID     int64                  `json:"run_id"`
	TaskSN    string                 `json:"task_sn"`
	Type      string                 `json:"type"`
	Proto     Protocol               `json:"proto"`
	Success   bool                   `json:"success"`
	Duration  time.Duration          `json:"duration"`
	LatencyMs float64                `json:"latency_ms"`
	Status    string                 `json:"status"`
	Metrics   map[string]interface{} `json:"metrics,omitempty"` // 详细指标：如 DNSTime, ConnectTime, HTTPStatus
	Err       error                  `json:"error,omitempty"`
}

// Prober 协议探测接口
type Prober interface {
	Probe(ctx context.Context, task *Task) (*Result, error)
}
