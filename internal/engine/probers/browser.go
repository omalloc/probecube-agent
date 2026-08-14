package probers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/omalloc/probecube-agent/internal/engine"
)

// BrowserProber 使用 Headless Chrome 加载页面并采集 Web Vitals 指标。
//
// 设计说明:设计文档最初规划 playwright-go,但其运行时依赖 Node.js 驱动与
// 预装浏览器,与边缘节点轻量静态二进制(alpine + CGO_ENABLED=0)的部署形态
// 冲突;chromedp 直接通过 CDP 协议驱动 Chrome/Chromium,无 Node 依赖。
type BrowserProber struct {
	allocatorOpts []chromedp.ExecAllocatorOption
}

func NewBrowserProber() *BrowserProber {
	return &BrowserProber{
		// 边缘节点常以 root 运行于容器,需要关闭 sandbox。
		allocatorOpts: []chromedp.ExecAllocatorOption{
			chromedp.NoSandbox,
		},
	}
}

func init() {
	engine.Register(engine.Browser, NewBrowserProber())
}

// webVitalsScript 采集 Web Vitals 核心指标:FCP/LCP/CLS 与导航耗时。
const webVitalsScript = `(() => {
	const nav = performance.getEntriesByType('navigation')[0] || {};
	let fcp = 0;
	for (const p of performance.getEntriesByType('paint')) {
		if (p.name === 'first-contentful-paint') fcp = p.startTime;
	}
	let lcp = 0;
	const lcps = performance.getEntriesByType('largest-contentful-paint');
	if (lcps.length > 0) lcp = lcps[lcps.length - 1].startTime;
	let cls = 0;
	for (const s of performance.getEntriesByType('layout-shift')) cls += s.value || 0;
	return {
		fcp_ms: fcp,
		lcp_ms: lcp,
		cls: cls,
		dcl_ms: nav.domContentLoadedEventEnd || 0,
		load_ms: nav.loadEventEnd || 0,
		title: document.title || '',
		url: location.href,
	};
})()`

// browserMetrics 页面脚本返回值。
type browserMetrics struct {
	FCPMs  float64 `json:"fcp_ms"`
	LCPMs  float64 `json:"lcp_ms"`
	CLS    float64 `json:"cls"`
	DCLMs  float64 `json:"dcl_ms"`
	LoadMs float64 `json:"load_ms"`
	Title  string  `json:"title"`
	URL    string  `json:"url"`
}

func (m *browserMetrics) toMetrics() map[string]interface{} {
	return map[string]interface{}{
		"fcp_ms": m.FCPMs, "lcp_ms": m.LCPMs, "cls": m.CLS,
		"dcl_ms": m.DCLMs, "load_ms": m.LoadMs,
		"title": m.Title, "final_url": m.URL,
	}
}

// Probe 加载 target 页面并采集性能指标。
func (p *BrowserProber) Probe(ctx context.Context, task *engine.Task) (*engine.Result, error) {
	start := time.Now()

	timeout := task.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, p.allocatorOpts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	runCtx, runCancel := context.WithTimeout(browserCtx, timeout)
	defer runCancel()

	var metrics browserMetrics
	resp, err := chromedp.RunResponse(runCtx,
		chromedp.Navigate(task.Target),
		chromedp.ActionFunc(func(runCtx context.Context) error {
			return chromedp.Evaluate(webVitalsScript, &metrics).Do(runCtx)
		}),
	)

	duration := time.Since(start)
	result := &engine.Result{
		TaskID:    task.ID,
		RunID:     task.RunID,
		TaskSN:    task.TaskSN,
		Type:      task.Type,
		Proto:     task.Proto,
		Duration:  duration,
		LatencyMs: float64(duration.Microseconds()) / 1000.0,
		Metrics:   metrics.toMetrics(),
	}

	if err != nil {
		if isExecNotFound(err) {
			return nil, fmt.Errorf("browser executable not found, install chromium: %w", err)
		}
		result.Success = false
		result.Status = "Load Failed"
		result.Err = err
		return result, nil
	}

	status := int64(0)
	if resp != nil {
		status = resp.Status
	}
	result.Metrics["http_status"] = status
	result.Success = status >= 200 && status < 400
	result.Status = fmt.Sprintf("%d", status)

	// 浏览器探测的时延以 load 事件为准,更贴近用户体感。
	if metrics.LoadMs > 0 {
		result.LatencyMs = metrics.LoadMs
	}
	return result, nil
}

func isExecNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "executable file not found") ||
		strings.Contains(msg, "no Chrome installation found") ||
		strings.Contains(msg, "fork/exec")
}

// browserExecutable 返回系统可用的 Chrome/Chromium 路径,供启动前检查与测试跳过。
func browserExecutable() string {
	candidates := []string{
		"headless_shell", "headless-shell", "chromium", "chromium-browser",
		"google-chrome", "google-chrome-stable", "google-chrome-beta", "google-chrome-unstable",
		"/usr/bin/google-chrome", "/usr/local/bin/chrome", "/snap/bin/chromium", "/usr/bin/chromium",
	}
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			return name
		}
	}
	return ""
}
