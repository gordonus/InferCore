# InferCore

InferCore is an open-source **AI Inference Control Plane**: a decision layer that sits **above** model serving and data plane systems.

It provides intelligent routing, cost-aware decisions, fallback and degrade orchestration, AI-native SLO signals, multi-tenant policy enforcement, and observability / scaling-signal exports.

**InferCore is not a model server.** It does not run token generation and does not replace vLLM, Triton, Ray Serve, or KServe.

## Design goals

InferCore fills the missing **system decision layer** in typical serving stacks:

| Goal | Meaning |
|------|---------|
| **Route** | Send different requests to different backends/models |
| **Protect** | Degrade gracefully on timeout, overload, or failure |
| **Optimize** | Trade off latency, quality, and cost with explicit policy |
| **Signal** | Export TTFT, TPOT, queue depth, fallback rate, and related AI-native metrics |
| **Isolate** | Tenant priority, quota, budget, and fairness |

## System boundaries

### InferCore owns

- Request ingress and normalization
- Routing decisions
- Tenant and policy enforcement
- Fallback and reliability orchestration
- Cost estimation and budget-based routing
- AI SLO signal generation
- Metrics / traces / events export

### InferCore does not own

- Model inference execution
- CUDA/GPU kernel optimization
- Training and MLOps pipelines
- Executing autoscaling (it can **export signals** for HPA/KEDA/custom autoscalers)
- Advanced dashboards and long-term analytics stores

## Architecture

```text
Client / SDK
    ↓
[ InferCore Gateway ]
    ↓
[ Request Normalizer ]
    ↓
[ Policy Engine ] ← tenant / budget / priority / guardrails
    ↓
[ Router Engine ] ← route selection / fallback planning / cost-aware decision
    ↓
[ Execution Adapter Layer ] ← vLLM / OpenAI-compatible HTTP / Mock / …
    ↓
Inference Backends

Parallel outputs:
- Metrics exporter (Prometheus text on /metrics)
- Trace / event exporters (configurable telemetry backends)
- In-memory AI SLO engine (bounded store; snapshots on responses)
- Scaling signals (/status.scaling_signals + infercore_scaling_* gauges)
```

## Request lifecycle

1. Client sends an inference request to InferCore (`POST /infer`).
2. Gateway parses tenant, task type, priority, and payload.
3. Policy engine evaluates quota, budget, priority, and guardrails.
4. Router selects a backend using rules, health, overload state, and optional cost optimization.
5. Execution adapter invokes the selected backend.
6. On timeout or classified failure, reliability rules may trigger fallback or degrade behavior.
7. InferCore records TTFT/TPOT/latency/fallback (where available) and exports metrics, events, and traces as configured.

## Differentiators

1. **Cost-aware routing** — Not only “can this run?” but “should this use the expensive model?” within budget and compatibility constraints.
2. **AI-native SLO** — TTFT, TPOT, completion latency, fallback markers; rolling hints exposed for operators (`/status`, Prometheus).
3. **Reliability orchestration** — Per-backend timeouts, configurable fallback chains, overload reject/degrade, health-aware routing.
4. **Multi-tenant policy** — Tenant classes, priorities, per-request budget estimates, per-tenant RPS windows (in-memory per replica).
5. **Scaling signals** — Queue depth, timeout-spike hints, TTFT degradation ratio, recent fallback rate, optional `max_concurrency` hints from config — intended for HPA/KEDA or custom autoscalers (**per-replica** unless you add shared state).

### Related products

- **InferCore** — runs the **decision layer** (how requests are routed, protected, and metered).
- **MicroWatch** (if you use it in your stack) — oriented toward **deep observability and analysis** (what happened, why, cost/SLO health). One-line split: *InferCore runs the plane; MicroWatch explains the run.*

## Repository layout

This repository includes:

- In-scope/out-of-scope document (`docs/scope.md`)
- YAML configuration draft
- OpenAPI draft for key endpoints
- Go interface contracts for core modules
- Minimal runnable HTTP service

## Quickstart

### Prerequisites

- Go 1.22+

### Run

```bash
go run ./cmd/infercore
```

The service starts on `:8080`.

Use a custom config file:

```bash
INFERCORE_CONFIG=./configs/infercore.example.yaml go run ./cmd/infercore
```

### Make targets

```bash
make help     # list targets
make all      # fmt, vet, test
make test
make build    # writes bin/infercore
make run      # CONFIG=... optional (default configs/infercore.example.yaml)
```

### Docker

```bash
make docker-build   # image tag: infercore:local
docker run --rm -p 8080:8080 infercore:local
```

Mount your own config:

```bash
docker run --rm -p 8080:8080 \
  -v "$(pwd)/configs/my.yaml:/app/config.yaml:ro" \
  -e INFERCORE_CONFIG=/app/config.yaml \
  infercore:local
```

### CI

GitHub Actions workflow (on `push`/`pull_request` to `main` or `master`): `.github/workflows/ci.yml` runs `go vet` and `go test ./...`.

### Try APIs

Health:

```bash
curl -s http://localhost:8080/health
```

Status:

```bash
curl -s http://localhost:8080/status
```

Infer:

```bash
curl -s -X POST http://localhost:8080/infer \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "team-a",
    "task_type": "simple",
    "priority": "normal",
    "input": {"text": "Summarize this article"},
    "options": {"stream": false, "max_tokens": 256}
  }'
```

Example success response (trimmed):

```json
{
  "request_id": "550e8400-e29b-41d4-a716-446655440000",
  "trace_id": "7f5f6f0d9f984f9a8e3d7b4f8f1d2a3c",
  "selected_backend": "small-model",
  "route_reason": "standard-simple-route",
  "status": "success"
}
```

Metrics:

```bash
curl -s http://localhost:8080/metrics
```

Smoke test script:

```bash
bash ./scripts/smoke.sh
```

## Core Endpoints

- `POST /infer`
- `GET /health`
- `GET /status`
- `GET /metrics`

API draft: `api/openapi.yaml`

## Current runtime behavior

- YAML config loading at startup (`INFERCORE_CONFIG` or default example file)
- `server.request_timeout_ms`: wall-clock budget for each `/infer` after JSON validation (policy + routing + backend execution); when **this** budget is exceeded returns **504** with `error.code=gateway_timeout` (per-backend `timeout_ms` expiring first remains **502** `execution_failed`). `cmd/infercore` sets `http.Server` `ReadTimeout` / `WriteTimeout` / `IdleTimeout` from `server.http.{read,write,idle}_timeout_ms` when set, otherwise from `request_timeout_ms` plus conventional slack (body read / write / keep-alive)
- Basic tenant policy enforcement:
  - unknown tenant rejection
  - per-request budget gate (lightweight estimate)
  - per-tenant RPS rate limit (in-memory, per-second window)
  - priority normalization from tenant defaults
- Rule-based routing by tenant class/task type/priority
- Routing skips backends that fail `adapter.Health` (cached per `server.health_cache_ttl_ms`; same cache backs `/status` backend status); if the default backend is unhealthy, the first healthy backend in config order is used (`healthy-fallback-order`); if none are healthy, `/infer` returns `route_error`
- Overload: `reliability.overload.queue_limit` + `action` (`reject` → 503 `overload`, `degrade` → skip cost optimization and annotate `degrade` in the JSON response); in-flight gauge `infercore_infer_inflight` on `/metrics`; `/status.queue_depth` reflects the same counter
- Lightweight cost optimization that can switch to a cheaper compatible backend within budget (only among healthy backends)
- Timeout-aware execution with fallback chain from reliability config
- Structured event logs for policy rejection, execution failure, and fallback trigger
- `/infer` response includes `policy_reason` and `effective_priority` for easier debugging
- `/infer` success response includes `trace_id` for trace correlation
- Error responses are structured as `{request_id,status,error:{code,message}}`
- HTTP metrics include path/method/status labels (`infercore_http_requests_total`)
- `priority` can be omitted in `/infer`; policy engine normalizes it from tenant defaults
- Config loader validates backend/tenant uniqueness and route/fallback backend references
- Config loader also validates routing rule name uniqueness and non-empty fallback triggers
- Allowed fallback triggers are validated: `timeout`, `backend_unhealthy`, `backend_error`
- Backend adapters: `mock`; OpenAI-compatible HTTP via `vllm`, `openai`, or `openai_compatible` (same implementation — Chat Completions + optional `GET` health)
- OpenAI-compatible backends support `api_key` (default `Authorization: Bearer …`), optional `auth_header_name` for raw key headers, `headers` map, `health_path` (default `/health`; use `/v1/models` for many cloud APIs), and `default_model`
- OpenAI-compatible adapter: `options.stream=true` uses upstream SSE when supported; JSON responses are treated as `stream_degraded`; InferCore still returns aggregated JSON to the client (see `docs/streaming-and-fallback.md`)
- `/infer` response includes `degrade` when upstream degrades streaming
- In-memory SLO engine (bounded by `slo.max_records` / `slo.max_age_ms`) records TTFT (from adapter), TPOT (streaming adapters when available), completion latency, and fallback markers
- Telemetry exporter emits metric/event logs for completed inference requests
- Basic trace hooks emit per-request trace records with trace/span IDs and result labels
- Telemetry: `log`, `otlp-http-stub`, `otlp-http` (OpenTelemetry OTLP/HTTP protobuf for standard Collectors), `otlp-http-json` (legacy non-standard JSON)
- Runtime telemetry switches: `telemetry.metrics_enabled` and `telemetry.tracing_enabled`
- OTLP flush/timeout via `otlp_flush_interval_ms`, `otlp_timeout_ms` (SDK batching for `otlp-http`)
- `/status` includes telemetry exporter status summary and `scaling_signals` (queue depth, timeout spike hint, rolling TTFT/fallback aggregates from the in-memory SLO store, `max_concurrency` hints from config)
- `/metrics` uses Prometheus `client_golang` (names unchanged: `infercore_requests_total`, `infercore_infer_inflight`, `infercore_http_requests_total`, plus `infercore_scaling_*` gauges mirroring scaling signals)
- Optional gate: `server.infercore_api_key` or `INFERCORE_API_KEY` — send `X-InferCore-Api-Key` or `Authorization: Bearer …` on `/infer`, `/status`, `/metrics` (`/health` stays open)
- Process waits for SIGINT/SIGTERM and shuts down HTTP server + OTLP flush (`Server.Shutdown`)

## Multi-instance deployment

Horizontal scale is typically **multiple InferCore replicas behind a load balancer**. Each process keeps its own in-memory policy windows, SLO store, overload counter, and health cache, so **global** RPS limits, concurrency caps, and rolling SLO ratios are **per replica** unless you add shared state (e.g. Redis) or aggregate in Prometheus. Use `/metrics` and `/status.scaling_signals` as inputs to cluster-level autoscaling.

## Project Structure

- `cmd/infercore`: service entrypoint
- `internal/server`: HTTP handlers and routing
- `internal/interfaces`: core module contracts
- `internal/types`: shared core data structures
- `configs`: YAML configuration examples
- `docs`: architecture and scope docs
- `api`: OpenAPI contract draft

## Load testing

- Guide: `docs/load-testing.md` (throughput with [hey](https://github.com/rakyll/hey), overload / rate-limit / 504 behavior)
- Config: `configs/infercore.loadtest.yaml` (mock-only, high `queue_limit`, `rate_limit_rps: 0`)
- Script: `make load-infer` or `bash ./scripts/load-infer.sh` (env: `BASE_URL`, `DURATION`, `CONCURRENCY`, `QPS`, `INFERCORE_API_KEY`)

## Documents

- Architecture (full one-pager copy for print/PDF): `docs/architecture-one-pager.md`
- Scope: `docs/scope.md`
- Config example: `configs/infercore.example.yaml`
- Observability: `docs/observability.md`
- Load testing: `docs/load-testing.md`
- Streaming & fallback: `docs/streaming-and-fallback.md`
- Planned hardening: `docs/future-work.md`

## Next Implementation Steps

1. Optional Prometheus scrape endpoint; OTLP Logs for telemetry events.
2. Extend policy with quota windows and richer guardrail hooks.
3. Add benchmark scenarios and baseline-vs-infercore scripts.
4. Client-facing SSE from InferCore (passthrough/proxy streaming).
5. Add config versioning, hot reload, and migration tooling.
