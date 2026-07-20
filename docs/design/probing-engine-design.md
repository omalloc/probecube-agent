# 边缘拨测引擎 (Probing Engine) 设计方案

## 1. 项目背景
设计一个高性能、可扩展的边缘节点拨测引擎，支持 ICMP, TCP/UDP, HTTP/HTTPS, DNS 及 Headless Browser 拨测，用于监测网络质量和应用性能。

## 2. 核心架构

### 2.1 目录结构 (基于 golang-project-layout)
```text
.
├── cmd/
│   └── prober/             # 引擎启动入口
├── internal/
│   ├── scheduler/          # 任务调度与并发控制
│   ├── probers/            # 协议探测实现 (ICMP, HTTP, etc.)
│   ├── engine/             # 核心引擎逻辑
│   └── collector/          # 结果采集与指标计算
├── pkg/
│   └── rawnet/             # 底层 Raw Socket 封装
└── api/                    # 探测任务定义 (GRPC/JSON)
```

### 2.2 核心接口定义
```go
type Prober interface {
    Probe(ctx context.Context, task *Task) (*Result, error)
}
```

## 3. 详细设计

### 3.1 高并发调度
- **并发控制**: 使用 `x/sync/errgroup` 配合 `chan struct{}` 信号量限制全局最大 Goroutine 数量。
- **超时管理**: 每个探测任务分配一个带 `WithTimeout` 的 `context.Context`。
- **生命周期**: 确保探测完成后所有资源（连接、文件描述符、浏览器实例）被正确释放。

### 3.2 协议实现
- **ICMP**: 
    - 使用 `golang.org/x/net/icmp`。
    - 支持非特权模式 (UDP-based) 和特权模式 (Raw Socket)。
- **TCP/UDP**:
    - 使用 `net.Dialer`。
    - **网络特征模拟**: 通过 `Dialer.LocalAddr` 绑定特定网卡/源 IP。
- **HTTP/HTTPS**:
    - 自定义 `http.Transport`，禁用连接池 (`DisableKeepAlives: true`) 以模拟冷启动访问。
    - 采集 `httptrace.ClientTrace` 指标：DNS、Connect、TLSHandshake、FirstByte。
    - 证书校验：在 `TLSNextProto` 中提取证书过期时间。
- **DNS**:
    - 使用 `github.com/miekg/dns`。
    - 支持 A, AAAA, CNAME, TXT 等记录校验，检测解析时延及劫持。
- **Browser**:
    - 集成 `playwright-go`。
    - 获取 LCP, FID, CLS 等 Web Vitals 指标。

### 3.3 网络特征模拟 (Raw Socket)
- **底层构造**: 使用 `google/gopacket` 或直接操作 `AF_PACKET` (Linux) / `BPF` (macOS) 构造特定的 TCP 标志位组合（如 SYN 扫描）。
- **绕过防火墙**: 模拟不同的 TTL 或 TCP Window Size。

## 4. 性能优化 (基于 golang-performance)
- **Zero-allocation**: 对高频探测结果使用 `sync.Pool`。
- **Transport 复用**: 虽然模拟首访需要关闭长连接，但对于大规模探测，按需复用 `Resolver` 等重型对象。
- **GOMEMLIMIT**: 在容器环境下设置内存限制，防止因大量并发探测导致的 OOM。

## 5. 验证与测试
- **Mock 服务**: 编写 Mock HTTP/DNS 服务器模拟各种网络异常（超时、劫持、5xx）。
- **Benchmark**: 压测单节点支持的每秒探测数 (PPS)。
- **Race Detection**: 使用 `go test -race` 验证并发安全性。
