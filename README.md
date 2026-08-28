# ProbeCube Agent

[![Go](https://img.shields.io/badge/Go-1.25.7-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

English · [简体中文](README.zh-CN.md)

ProbeCube Agent is the distributed probing worker for the ProbeCube monitoring platform. Deploy it close to users, edge sites, or private networks to measure real network paths. The agent receives jobs over NATS, executes them under a bounded concurrency limit, and reports structured results to the ProbeCube control plane.

It currently provides ICMP, TCP SYN, UDP, HTTP/HTTPS, and DNS probes, including protocol-specific latency and diagnostic metadata.

> [!IMPORTANT]
> This repository is under active development. The current runtime expects an AES-GCM-encrypted configuration embedded at build time. Read [Configuration and secrets](#configuration-and-secrets) before building a runnable image.

## Why ProbeCube Agent?

- **Observe from where traffic really originates** — place agents in regions, edge POPs, offices, or private networks instead of relying on a single central probe.
- **Use one worker for multiple protocols** — inspect reachability, port behavior, DNS resolution, and web request phases with a consistent result model.
- **Control resource usage** — bound in-flight work with a configurable worker pool and per-task timeouts.
- **Target one node or broadcast** — consume both shared tasks and node-specific NATS subjects.
- **Keep transport contracts explicit** — jobs and results use versioned Protocol Buffers over NATS.
- **Extend the engine in Go** — add a prober by implementing one small interface and registering it by protocol.

## How it works

```text
                         NATS
  ProbeCube Console  ─────────────►  ProbeCube Agent
      control plane    task (PB)       │
                                          ├─ bounded scheduler
                                          ├─ ICMP / TCP / UDP
                                          ├─ HTTP(S) / DNS
                                          └─ network discovery
                               ◄──────────┘
                          result + heartbeat (PB/headers)
```

At startup, the agent establishes its NATS identity from a persistent machine ID, waits for the messaging connection, and starts the scheduler. It then:

1. subscribes to broadcast and node-specific task subjects;
2. converts incoming Protobuf messages into engine tasks;
3. dispatches each task to the matching prober under the configured concurrency limit;
4. publishes results with the task, run, protocol, node, latency, status, metrics, and error context;
5. discovers local/public addresses through STUN and sends a heartbeat every two seconds.

## Supported probes

| Protocol | Target format | What it measures | Runtime notes |
| --- | --- | --- | --- |
| ICMP | `example.com` or `1.1.1.1` | Echo RTT and sequence | Uses raw ICMP as root; otherwise uses unprivileged `udp4` ping where supported. IPv4 only. |
| TCP | `example.com:443` | SYN response time; distinguishes SYN-ACK and RST | Raw IPv4 socket; requires root or `CAP_NET_RAW`. A RST still proves the host is reachable and is reported as alive. |
| UDP | `host:port` | Send/read duration and byte counts | Sends task payload, or `PING` by default. A timeout after a successful send is reported as `Sent (no response)` because UDP is connectionless. |
| HTTP / HTTPS | Absolute URL, such as `https://example.com/health` | Total latency, DNS, connect, TLS, first-byte timing, HTTP status, and certificate expiry | Sends `GET`, disables keep-alive, follows Go's default redirect policy, and treats 2xx/3xx as success. TLS certificate verification is currently disabled. |
| DNS | Domain name, such as `example.com` | Query RTT, response code, and IPv4 answers | Sends an A query. Set `metadata.server` to `host:port`; defaults to `8.8.8.8:53`. |

`Browser` exists in the shared protocol enum but no browser prober is registered yet. Such a task currently returns `Unsupported Protocol`.

## Requirements

- Go 1.25.7 or newer compatible toolchain for source builds
- A reachable NATS server
- A valid NKey seed if the NATS deployment requires NKey authentication
- Linux and root/`CAP_NET_RAW` for TCP SYN probing
- Permission to create `/var/lib/probecube-agent/machine-id` if node identity must persist across restarts
- An encrypted `cmd/probecube-agent/app.cert` and its matching build-time key

## Quick start

### Prepare the configuration

Start from [`configs/config.yaml`](configs/config.yaml):

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

`pool_size` must be greater than zero. `async: true` queues jobs before dispatch; `false` dispatches directly from the subscription callback. The `worker.timeout` and `worker.interval` fields are part of the current configuration schema, while each received task supplies the timeout used by the built-in probers.

### Configuration and secrets

The executable does not currently read `configs/config.yaml` at runtime. Your release process must:

1. encrypt the final YAML with AES-GCM using a 16-, 24-, or 32-byte key;
2. store the binary output as `cmd/probecube-agent/app.cert` in the format `nonce || ciphertext-and-tag`;
3. pass the same key as a hexadecimal string through `PROBECUBE_DESKEY` when building.

Both `*.cert` and `*.key` are ignored by Git. Never commit the NKey seed or encryption key.

### Build and run

```bash
PROBECUBE_DESKEY='<hex-encoded-aes-key>' make build
sudo install -d -m 0755 /var/lib/probecube-agent
sudo ./bin/probecube-agent -embed
```

The process should connect to NATS and begin publishing heartbeats. If it exits during configuration loading, verify that `app.cert` was encrypted with the key supplied to `make build`.

### Docker

The Dockerfile uses BuildKit secrets so the encryption key is not persisted as an image build argument:

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

Use `--network host` only when the deployment requires the agent to observe the host network namespace directly and your security model permits it.

## Configuration reference

| Path | Type | Description |
| --- | --- | --- |
| `server.http.network` | string | Optional network passed to the Kratos HTTP server. |
| `server.http.addr` | string | HTTP listener. `localhost:0` binds a random loopback port. No public routes are currently registered. |
| `server.http.timeout` | duration | HTTP server timeout. |
| `leafnode.endpoint` | list of strings | One or more NATS server URLs. |
| `leafnode.nkeys` | string | Raw NKey seed used to sign the NATS connection. Leave empty only when the server allows unauthenticated clients. |
| `worker.pool_size` | integer | Maximum number of probes executing concurrently. Must be positive. |
| `worker.async` | boolean | Whether incoming tasks first enter the internal buffered queue. |
| `worker.timeout` | duration | Reserved worker-level timeout in the current schema. Built-in probers use the timeout carried by each task. |
| `worker.interval` | duration | Reserved synchronization interval in the current schema. |

Go-style durations such as `500ms`, `5s`, and `1m` are accepted.

## Messaging contract

The agent exchanges Protocol Buffer payloads defined by [`probecube-console/api/message/v1`](https://github.com/omalloc/probecube-console/tree/main/api/message/v1).

| Subject | Direction | Purpose |
| --- | --- | --- |
| `dialsys.probe.task` | Console → agent | Broadcast probe task; all subscribed agents receive it. |
| `dialsys.probe.task.<node-sn>` | Console → agent | Task targeted to one persistent node serial number. |
| `dialsys.probe.results` | Agent → console | Probe result. |
| `dialsys.heartbeat` | Agent → console | Two-second heartbeat with `host`, public `ip`, private `priv_ip`, and `sn` headers. |
| `dialsys.node.metadata` | Agent ↔ console | Request/reply lookup for the agent's node metadata, refreshed every minute. |

Published messages include `content-type: application/protobuf`, `schema-version: v1`, and the agent `sn`. The persistent SN is loaded from `/var/lib/probecube-agent/machine-id`; if the file is absent, the agent derives a machine identifier and attempts to write it there.

The heartbeat public/private address pair is refreshed every minute. Discovery uses UDP STUN and tries `stun.miwifi.com:3478` followed by `stun.cloudflare.com:3478`.

## Extending the probing engine

Implement `engine.Prober`, register it during package initialization, and ensure the package is imported by the scheduler:

```go
type CustomProber struct{}

func (p *CustomProber) Probe(ctx context.Context, task *engine.Task) (*engine.Result, error) {
	// Execute the protocol and return a structured result.
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

The scheduler restores `TaskID`, `RunID`, and `TaskSN` on successful plugin results. A prober should still populate protocol, type, timing, success, status, metrics, and error details. See [the plugin guide](docs/design/plugin.md) and the built-in implementations in [`internal/engine/probers`](internal/engine/probers).

## Project layout

```text
cmd/probecube-agent/        application entrypoint and Wire assembly
configs/                    example configuration (never store real secrets)
docs/design/                probing-engine and plugin design notes
internal/conf/              generated configuration schema
internal/engine/            task/result model, registry, collector
internal/engine/probers/    built-in protocol implementations
internal/messaging/         NATS client, publisher, subscriber, node identity
internal/server/            HTTP server and bounded task scheduler
pkg/embedbuf/               encrypted embedded-config source
pkg/netinfo/                STUN-based public/local IP discovery
```

## Development

For compile-only work, an empty ignored embed file is sufficient; a valid encrypted file is required to run the process:

```bash
touch cmd/probecube-agent/app.cert
go test ./...
go test -race ./...
```

Useful generation and build commands:

```bash
make init       # install Wire and Buf
make config     # regenerate configuration protobufs
make api        # regenerate API/OpenAPI artifacts
make all        # run all generation steps and go mod tidy
make build      # build bin/probecube-agent (requires PROBECUBE_DESKEY)
```

Generated `*.pb.go` and `wire_gen.go` files should not be edited by hand. New Go source comments should follow the project's existing Chinese-comment convention.

## Operational and security notes

- Raw sockets materially increase process privileges. Prefer adding only `CAP_NET_RAW` over running an unrestricted privileged container.
- HTTPS probing currently sets `InsecureSkipVerify`; certificate dates are observed, but trust-chain and hostname validation failures do not fail a probe.
- UDP success means the datagram was sent; a silent target cannot be distinguished from a filtered path without a protocol-specific response.
- STUN discovers the public address seen by an outbound server. It does not prove inbound reachability or port forwarding.
- Result publication currently logs an error but does not persist or retry failed reports.
- The internal task and result buffers are finite (100 and 1000 entries respectively); size the worker pool and upstream dispatch rate accordingly.

## Roadmap

- Browser/Web Vitals probing
- Configurable HTTP methods, headers, bodies, and TLS verification policy
- Richer DNS record types and assertions
- Durable result retry and delivery observability
- Runtime configuration that does not require rebuilding the binary
- Metrics, health endpoints, and production deployment manifests

Roadmap items describe intended work, not capabilities in the current release.

## Related projects

- [ProbeCube Console](https://github.com/omalloc/probecube-console) — control plane, task dispatch, result processing, and APIs
- [ProbeCube UI](https://github.com/omalloc/probing-ui-v2) — web interface for managing and visualizing probes

## Contributing

Issues and pull requests are welcome. Before submitting a change:

1. keep protocol and messaging compatibility explicit;
2. add focused tests beside the package being changed;
3. run `go test ./...` and, for concurrent code, `go test -race ./...`;
4. regenerate code through the Make targets rather than editing generated files;
5. use a Conventional Commit prefix such as `feat:`, `fix:`, `docs:`, or `test:`.

For security-sensitive reports, avoid posting credentials, embedded configuration, node identifiers, or private network details in a public issue.

## License

ProbeCube Agent is distributed under the [MIT License](LICENSE).
