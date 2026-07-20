package probers

import (
	"context"
	"time"

	"github.com/miekg/dns"

	"github.com/omalloc/probecube-agent/internal/engine"
)

type DNSProber struct{}

func NewDNSProber() *DNSProber {
	return &DNSProber{}
}

func init() {
	engine.Register(engine.DNS, NewDNSProber())
}

func (p *DNSProber) Probe(ctx context.Context, task *engine.Task) (*engine.Result, error) {
	start := time.Now()

	// 1. 获取 DNS 服务器地址，默认使用 8.8.8.8
	server := task.Metadata["server"]
	if server == "" {
		server = "8.8.8.8:53"
	}

	// 2. 构造查询报文
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(task.Target), dns.TypeA)
	m.RecursionDesired = true

	c := new(dns.Client)
	c.Timeout = task.Timeout

	// 3. 执行查询
	in, rtt, err := c.Exchange(m, server)
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
		result.Success = false
		result.Status = "Query Failed"
		result.Err = err
		return result, nil
	}

	// 4. 解析结果
	result.Metrics["rtt_ms"] = float64(rtt.Microseconds()) / 1000.0
	result.Metrics["rcode"] = dns.RcodeToString[in.Rcode]

	var ips []string
	for _, a := range in.Answer {
		if t, ok := a.(*dns.A); ok {
			ips = append(ips, t.A.String())
		}
	}
	result.Metrics["answers"] = ips

	if in.Rcode == dns.RcodeSuccess {
		result.Success = true
		result.Status = "Success"
	} else {
		result.Success = false
		result.Status = dns.RcodeToString[in.Rcode]
	}

	return result, nil
}
