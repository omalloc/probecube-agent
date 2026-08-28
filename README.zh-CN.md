# ProbeCube Agent

[![Go](https://img.shields.io/badge/Go-1.25.7-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

[English](README.md) · 简体中文

ProbeCube Agent 是 ProbeCube 监控平台的分布式拨测执行端。你可以把它部署在用户附近、边缘站点或私有网络中，观测真实访问路径。Agent 通过 NATS 接收任务，在受控并发下执行拨测，并将结构化结果上报给 ProbeCube 控制面。

当前已实现 ICMP、TCP SYN、UDP、HTTP/HTTPS 和 DNS 拨测，并提供各协议对应的延迟与诊断元数据。

> [!IMPORTANT]
> 本项目仍在持续开发中。当前运行时要求在构建阶段嵌入经过 AES-GCM 加密的配置。构建可运行镜像前，请先阅读[配置与密钥](#配置与密钥)。

## 为什么选择 ProbeCube Agent？

- **从真实流量发生的位置观测**：将 Agent 部署到不同地域、边缘 POP、办公网络或私有网络，不再局限于单个中心探测点。
- **一个执行端覆盖多种协议**：通过统一结果模型检查连通性、端口行为、DNS 解析和 Web 请求阶段。
- **资源使用可控**：通过可配置的 Worker Pool 限制并发任务数，并为每个任务设置超时。
- **支持单节点定向与广播**：同时消费共享任务和节点专属的 NATS Subject。
- **传输契约明确**：任务与结果使用版本化 Protocol Buffers，并通过 NATS 传输。
- **可用 Go 扩展探测能力**：只需实现一个小型接口并注册协议，即可增加 Prober。

## 工作原理

```text
                            NATS
  ProbeCube Console  ───────────────►  ProbeCube Agent
        控制面          任务（PB）       │
                                            ├─ 并发受控的调度器
                                            ├─ ICMP / TCP / UDP
                                            ├─ HTTP(S) / DNS
                                            └─ 网络地址发现
                                 ◄──────────┘
                             结果与心跳（PB/Header）
```

启动时，Agent 从持久化机器 ID 建立 NATS 身份，等待消息连接就绪并启动调度器。此后它会：

1. 订阅广播任务和节点定向任务；
2. 将收到的 Protobuf 消息转换为引擎任务；
3. 在配置的并发上限内，将任务分派给匹配的 Prober；
4. 上报包含任务、运行批次、协议、节点、延迟、状态、指标和错误上下文的结果；
5. 通过 STUN 发现本地/公网地址，并每两秒发送一次心跳。

## 已支持的拨测协议

| 协议 | Target 格式 | 采集内容 | 运行说明 |
| --- | --- | --- | --- |
| ICMP | `example.com` 或 `1.1.1.1` | Echo RTT 和序列号 | root 用户使用 Raw ICMP；其他用户在系统支持时使用非特权 `udp4` ping。当前仅支持 IPv4。 |
| TCP | `example.com:443` | SYN 响应时间，区分 SYN-ACK 与 RST | 使用 Raw IPv4 Socket，需要 root 或 `CAP_NET_RAW`。RST 同样可以证明主机可达，因此会标记为存活。 |
| UDP | `host:port` | 发送/读取耗时与字节数 | 发送任务 Payload，未提供时默认发送 `PING`。UDP 无连接，成功发送后的超时会记录为 `Sent (no response)`。 |
| HTTP / HTTPS | 完整 URL，例如 `https://example.com/health` | 总延迟、DNS、建连、TLS、首字节、HTTP 状态和证书到期时间 | 发送 `GET`，禁用 Keep-Alive，使用 Go 默认重定向策略，2xx/3xx 视为成功。当前未校验 TLS 证书。 |
| DNS | 域名，例如 `example.com` | 查询 RTT、响应码和 IPv4 答案 | 发起 A 记录查询。可通过 `metadata.server` 指定 `host:port`，默认使用 `8.8.8.8:53`。 |

共享协议枚举中已经包含 `Browser`，但当前尚未注册浏览器 Prober；此类任务会返回 `Unsupported Protocol`。

## 运行要求

- 源码构建需要 Go 1.25.7 或兼容的更新工具链
- 可访问的 NATS Server
- 如果 NATS 启用了 NKey 认证，需要有效的 NKey Seed
- TCP SYN 拨测需要 Linux 与 root/`CAP_NET_RAW`
- 如需节点身份跨重启持久化，需要有权限创建 `/var/lib/probecube-agent/machine-id`
- 加密后的 `cmd/probecube-agent/app.cert` 及其匹配的构建密钥

## 快速开始

### 准备配置

从 [`configs/config.yaml`](configs/config.yaml) 开始：

```yaml
server:
  http:
    addr: localhost:0
    timeout: 1s

leafnode:
  endpoint:
    - "nats://nats.example.com:4222"
  nkeys: "SU...YOUR_NKEY_SEED"

worker:
  pool_size: 10
  async: true
  timeout: 5s
  interval: 5s
```

`pool_size` 必须大于零。`async: true` 表示任务先进入内部队列再调度；设置为 `false` 时，会从订阅回调直接调度。`worker.timeout` 和 `worker.interval` 已存在于当前配置 Schema 中，内置 Prober 实际使用的是每个下发任务携带的超时时间。

### 配置与密钥

当前可执行文件不会在运行时读取 `configs/config.yaml`。发布流程必须完成以下工作：

1. 使用 16、24 或 32 字节密钥，通过 AES-GCM 加密最终 YAML；
2. 按 `nonce || ciphertext-and-tag` 的格式，将二进制结果保存为 `cmd/probecube-agent/app.cert`；
3. 构建时通过 `PROBECUBE_DESKEY` 传入同一密钥的十六进制字符串。

`*.cert` 与 `*.key` 均已加入 Git 忽略规则。切勿提交 NKey Seed 或加密密钥。

### 构建并运行

```bash
PROBECUBE_DESKEY='<hex-encoded-aes-key>' make build
sudo install -d -m 0755 /var/lib/probecube-agent
sudo ./bin/probecube-agent -embed
```

进程正常启动后会连接 NATS 并开始发送心跳。如果程序在配置加载阶段退出，请确认 `app.cert` 使用的密钥与传给 `make build` 的密钥一致。

### Docker

Dockerfile 使用 BuildKit Secret，避免将加密密钥作为镜像构建参数持久化：

```bash
export PROBECUBE_DESKEY='<hex-encoded-aes-key>'
DOCKER_BUILDKIT=1 docker build \
  --secret id=appkey,env=PROBECUBE_DESKEY \
  -t probecube-agent:local .

docker run --rm \
  --name probecube-agent \
  --cap-add NET_RAW \
  -v probecube-agent-state:/var/lib/probecube-agent \
  probecube-agent:local
```

只有在部署确实需要 Agent 直接观测宿主机网络命名空间，并且安全模型允许时，才应使用 `--network host`。

## 配置参考

| 配置路径 | 类型 | 说明 |
| --- | --- | --- |
| `server.http.network` | string | 可选，传给 Kratos HTTP Server 的 Network。 |
| `server.http.addr` | string | HTTP 监听地址。`localhost:0` 表示绑定随机回环端口。当前没有注册公开路由。 |
| `server.http.timeout` | duration | HTTP Server 超时时间。 |
| `leafnode.endpoint` | string 列表 | 一个或多个 NATS Server URL。 |
| `leafnode.nkeys` | string | 用于签名 NATS 连接的原始 NKey Seed。仅在 Server 允许匿名客户端时才能留空。 |
| `worker.pool_size` | integer | 同时执行的最大拨测数量，必须为正数。 |
| `worker.async` | boolean | 收到的任务是否先进入内部缓冲队列。 |
| `worker.timeout` | duration | 当前 Schema 中预留的 Worker 级超时；内置 Prober 使用任务携带的超时。 |
| `worker.interval` | duration | 当前 Schema 中预留的同步周期。 |

支持 `500ms`、`5s`、`1m` 等 Go Duration 格式。

## 消息契约

Agent 交换的 Protocol Buffer Payload 定义在 [`probecube-console/api/message/v1`](https://github.com/omalloc/probecube-console/tree/main/api/message/v1)。

| Subject | 方向 | 用途 |
| --- | --- | --- |
| `dialsys.probe.task` | Console → Agent | 广播拨测任务；所有订阅该 Subject 的 Agent 都会收到。 |
| `dialsys.probe.task.<node-sn>` | Console → Agent | 定向发送给一个持久化节点序列号的任务。 |
| `dialsys.probe.results` | Agent → Console | 拨测结果。 |
| `dialsys.heartbeat` | Agent → Console | 两秒一次的心跳，Header 包含 `host`、公网 `ip`、私网 `priv_ip` 与 `sn`。 |
| `dialsys.node.metadata` | Agent ↔ Console | 查询 Agent 节点元数据的 Request/Reply，每分钟刷新一次。 |

发布的消息包含 `content-type: application/protobuf`、`schema-version: v1` 和 Agent `sn`。持久化 SN 从 `/var/lib/probecube-agent/machine-id` 读取；文件不存在时，Agent 会生成机器标识并尝试写入该路径。

心跳中的公网/私网地址每分钟刷新一次。地址发现使用 UDP STUN，依次尝试 `stun.miwifi.com:3478` 与 `stun.cloudflare.com:3478`。

## 扩展拨测引擎

实现 `engine.Prober`，在包初始化阶段注册，并确保调度器导入对应包：

```go
type CustomProber struct{}

func (p *CustomProber) Probe(ctx context.Context, task *engine.Task) (*engine.Result, error) {
	// 执行协议拨测并返回结构化结果。
	return &engine.Result{
		Proto:   task.Proto,
		Success: true,
		Status:  "Success",
		Metrics: map[string]any{"example": "value"},
	}, nil
}

func init() {
	engine.Register(engine.Protocol("CUSTOM"), &CustomProber{})
}
```

对于成功返回的插件结果，调度器会恢复 `TaskID`、`RunID` 和 `TaskSN`。Prober 仍应填充协议、类型、耗时、成功状态、状态文本、指标与错误详情。更多说明请参见[插件指南](docs/design/plugin.md)以及 [`internal/engine/probers`](internal/engine/probers) 中的内置实现。

## 项目结构

```text
cmd/probecube-agent/        应用入口与 Wire 组装
configs/                    示例配置（禁止存放真实密钥）
docs/design/                拨测引擎与插件设计说明
internal/conf/              生成的配置 Schema
internal/engine/            任务/结果模型、注册表与采集器
internal/engine/probers/    内置协议实现
internal/messaging/         NATS 客户端、发布订阅与节点身份
internal/server/            HTTP Server 与并发受控的任务调度器
pkg/embedbuf/               加密嵌入配置的数据源
pkg/netinfo/                基于 STUN 的公网/本地 IP 发现
```

## 开发

仅编译和测试时，创建一个空的、被 Git 忽略的嵌入文件即可；真正运行进程仍需要有效的加密文件：

```bash
touch cmd/probecube-agent/app.cert
go test ./...
go test -race ./...
```

常用生成与构建命令：

```bash
make init       # 安装 Wire 与 Buf
make config     # 重新生成配置 Protobuf
make api        # 重新生成 API/OpenAPI 产物
make all        # 执行全部生成步骤与 go mod tidy
make build      # 构建 bin/probecube-agent（需要 PROBECUBE_DESKEY）
```

不要手工修改生成的 `*.pb.go` 与 `wire_gen.go`。新增 Go 源码注释应遵循项目现有的中文注释约定。

## 运维与安全说明

- Raw Socket 会显著提升进程权限。优先只授予 `CAP_NET_RAW`，不要使用不受限的特权容器。
- HTTPS 拨测当前设置了 `InsecureSkipVerify`；它会观测证书日期，但证书信任链或主机名校验失败不会导致拨测失败。
- UDP 成功只表示数据报已经发出；如果没有协议级响应，无法区分静默目标与被过滤的链路。
- STUN 只能发现出站 Server 观察到的公网地址，不能证明入站可达性或端口映射有效。
- 当前结果发布失败只会记录错误，不会持久化或重试上报。
- 内部任务与结果缓冲区容量有限（分别为 100 和 1000）；请根据上游下发速率合理设置 Worker Pool。

## 路线图

- Browser/Web Vitals 拨测
- 可配置的 HTTP Method、Header、Body 与 TLS 校验策略
- 更多 DNS 记录类型与断言
- 持久化结果重试与投递可观测性
- 无需重新构建二进制的运行时配置
- Metrics、健康检查接口与生产部署清单

以上路线图是计划方向，不代表当前版本已经具备这些能力。

## 相关项目

- [ProbeCube Console](https://github.com/omalloc/probecube-console)：控制面、任务下发、结果处理与 API
- [ProbeCube UI](https://github.com/omalloc/probing-ui-v2)：用于管理和可视化拨测的 Web 界面

## 参与贡献

欢迎提交 Issue 与 Pull Request。提交修改前请：

1. 明确维护协议与消息兼容性；
2. 在被修改的包旁增加聚焦测试；
3. 执行 `go test ./...`，并对并发代码执行 `go test -race ./...`；
4. 通过 Make Target 重新生成代码，不要直接修改生成文件；
5. 使用 `feat:`、`fix:`、`docs:`、`test:` 等 Conventional Commit 前缀。

报告安全问题时，请勿在公开 Issue 中粘贴凭据、嵌入配置、节点标识或私网信息。

## 许可证

ProbeCube Agent 使用 [MIT License](LICENSE) 发布。
