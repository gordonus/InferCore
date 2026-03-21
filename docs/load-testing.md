# Load testing InferCore

This guide covers **performance** (latency, RPS) and **behavior** under pressure (overload, rate limits) for the HTTP control plane.

## Prerequisites

1. **InferCore** running with a config suited to your scenario (see below).
2. **[hey](https://github.com/rakyll/hey)** for HTTP load:
   ```bash
   go install github.com/rakyll/hey@latest
   ```
   `scripts/load-infer.sh` will use `hey` from `PATH`, or from `$(go env GOBIN)/hey` / `$(go env GOPATH)/bin/hey` if those exist. To call `hey` yourself, add Go’s bin directory to `PATH`, e.g. `export PATH="$(go env GOPATH)/bin:$PATH"`.

   The script reads `PAYLOAD` with `cat` and passes it to `hey` as `-d "$BODY"`, because `hey -d @file` can send an **empty body** on some platforms (every `/infer` returns **400**).

## Quick start (throughput)

Use the mock-only profile to avoid slow/failing health checks against real endpoints:

```bash
# Terminal A
INFERCORE_CONFIG=configs/infercore.loadtest.yaml make run

# Terminal B
bash ./scripts/load-infer.sh
```

Environment knobs:

| Variable | Default | Meaning |
|----------|---------|---------|
| `BASE_URL` | `http://127.0.0.1:8080` | InferCore base URL |
| `DURATION` | `30s` | How long `hey` runs (`-z`) |
| `CONCURRENCY` | `50` | Concurrent workers (`-c`) |
| `QPS` | _(empty)_ | Optional rate cap (`hey -q`; empty = max throughput) |
| `PAYLOAD` | `scripts/loadtest-payload.json` | POST body for `/infer` |
| `INFERCORE_API_KEY` | _(empty)_ | Sent as `X-InferCore-Api-Key` if set |
| `WAIT_FOR_SERVER` | `0` | Seconds to retry `GET /health` before failing (e.g. `30` while `make run` starts) |
| `SKIP_PREFLIGHT` | _(unset)_ | Set to `1` to skip the `/health` check (not recommended) |

Makefile shortcut:

```bash
make load-infer
```

## What to read from `hey` output

- **Requests/sec** — sustained throughput your process achieved.
- **Status code distribution** — expect mostly `200` for successful `/infer`.
- **Latency** — p50/p90/p99; mock backends are sub-millisecond; real vLLM will dominate tail latency.
- **Errors** — connection resets or timeouts often mean `http.Server` read/write limits or OS limits (`ulimit -n`).

## Behavior checks

### 1) Overload (`503` + `overload`)

In config set:

```yaml
reliability:
  overload:
    queue_limit: 5
    action: reject
```

Run with high concurrency, e.g.:

```bash
CONCURRENCY=100 DURATION=15s bash ./scripts/load-infer.sh
```

Expect a mix of **503** and JSON `error.code: overload` when in-flight work hits the limit.

### 2) Per-tenant RPS (`429` + `policy_rejected`)

Set `tenants[].rate_limit_rps` to a small number (e.g. `5`) and either:

- Run `hey` with high QPS, or  
- Use multiple workers against the same `tenant_id` in the payload.

Expect **429** when the in-process per-second window is exceeded (per replica).

### 3) Global infer deadline (`504` + `gateway_timeout`)

Lower `server.request_timeout_ms` and use a slow backend or blocking adapter so work exceeds the budget. Expect **504** with `gateway_timeout` (not the same as per-backend **502**).

## Prometheus metrics during / after load

After a run, the script prints a slice of `/metrics`. Useful series:

- `infercore_requests_total` — accepted `/infer` count  
- `infercore_infer_inflight` — should return toward 0 after load stops  
- `infercore_http_requests_total{path="/infer",...}` — status mix (200 vs 503 vs 429)

For continuous scraping, point Prometheus at `GET /metrics` (and set `INFERCORE_API_KEY` on the scraper if enabled).

## Optional: vegeta

Example target file pattern:

```text
POST http://127.0.0.1:8080/infer
Content-Type: application/json
@scripts/loadtest-payload.json
```

```bash
vegeta attack -rate=200 -duration=30s -targets=targets.txt | vegeta report
```

## RAG load tests

The default payload is plain inference. To load-test **RAG**, use a JSON body with `request_type: rag`, `context.query` / `input.text`, and `context.knowledge_base` matching your config, and ensure `knowledge_bases` is enabled in the InferCore YAML (see the main README).

## Troubleshooting

### `connection refused` and huge RPS / `NaN` from `hey`

If InferCore is **not listening** on `BASE_URL`, every dial fails immediately. `hey` still counts those attempts, so you may see **absurd requests/sec**, **NaN** latencies, and on macOS **`socket: no buffer space available`** (too many failed connects in a short time).

**Fix:** start the server before load, or set `BASE_URL` to the correct host/port. The script preflights `GET /health` unless `SKIP_PREFLIGHT=1`.

### `address already in use` on `make run`

Another process is bound to that port (often a previous InferCore). Free the port or change `server.port` in your YAML.

## Multi-instance note

RPS limits and overload counters are **per process**. To test cluster behavior, run N replicas behind a load balancer and aggregate metrics in Prometheus.

## See also

- **Website:** [infercore.dev](https://infercore.dev) — product overview and blog
- [README.md](../README.md) — project index
