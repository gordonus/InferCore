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
- **RAG orchestration** (optional): retrieval + rerank after routing, then merge into the model payload (`request_type: rag`; file-backed knowledge bases in v1.5)
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
┌──────────────────────────────────────────────────────────────────────────┐
│ Reliability · overload admission                                         │
│   • reliability.overload.queue_limit + action: reject (503) | degrade    │
│   • Degrade: skip cost optimization, annotate response; router sees      │
│     overload state (queue depth / degrade) for routing hints             │
└──────────────────────────────────────────────────────────────────────────┘
    ↓
[ Router Engine ] ← route rules / health-aware backend choice / cost-aware pick
    ↓
┌──────────────────────────────────────────────────────────────────────────┐
│ Optional RAG (request_type=rag, when configured)                         │
│   retrieve (KB) → rerank (e.g. noop) → merge into input.*                │
└──────────────────────────────────────────────────────────────────────────┘
    ↓
┌──────────────────────────────────────────────────────────────────────────┐
│ Reliability · execution & fallback                                       │
│   • ExecuteWithFallback: primary backend then fallback_rules chain       │
│   • Triggers: timeout, backend_unhealthy, backend_error                  │
│   • Per-backend timeout_ms; bounded by gateway request_timeout_ms        │
│   • Streaming: stream_fallback_enabled when options.stream               │
└──────────────────────────────────────────────────────────────────────────┘
    ↓
[ Execution Adapter Layer ] ← OpenAI-compatible (e.g. OpenAI, vLLM)  / Gemini / Mock / …
    ↓
Inference Backends

Parallel outputs:
- Metrics exporter (Prometheus text on /metrics)
- Trace / event exporters (configurable telemetry backends)
- In-memory AI SLO engine (bounded store; snapshots on responses)
- Scaling signals (/status.scaling_signals + infercore_scaling_* gauges)
```

## Request lifecycle

1. Client sends an AI request to InferCore (`POST /infer`) — same endpoint for **inference**, **RAG**, and **agent (preview)**.
2. Gateway parses tenant, task type, priority, and payload.
3. Policy engine evaluates quota, budget, priority, and guardrails.
4. Overload admission (`reliability.overload`) may reject or degrade before routing.
5. Router selects a backend using rules, health, overload state, and optional cost optimization.
6. **If `request_type` is `rag` and `knowledge_bases` are configured**, InferCore runs **retrieve** then **rerank**, and merges retrieved text into the payload (e.g. `input.retrieved_context`) before the model call.
7. Execution adapter invokes the selected backend.
8. On timeout or classified failure, reliability rules may trigger fallback or degrade behavior.
9. InferCore records TTFT/TPOT/latency/fallback (where available) and exports metrics, events, and traces as configured.

## Differentiators

1. **Cost-aware routing** — Not only “can this run?” but “should this use the expensive model?” within budget and compatibility constraints.
2. **AI-native SLO** — TTFT, TPOT, completion latency, fallback markers; rolling hints exposed for operators (`/status`, Prometheus).
3. **Reliability orchestration** — Per-backend timeouts, configurable fallback chains, overload reject/degrade, health-aware routing.
4. **Multi-tenant policy** — Tenant classes, priorities, per-request budget estimates, per-tenant RPS windows (in-memory per replica).
5. **Scaling signals** — Queue depth, timeout-spike hints, TTFT degradation ratio, recent fallback rate, optional `max_concurrency` hints from config — intended for HPA/KEDA or custom autoscalers (**per-replica** unless you add shared state).

## Repository layout

This repository includes:

- YAML configuration examples and OpenAPI draft (`api/openapi.yaml`)
- Go implementation of the control plane (policy, routing, reliability, RAG retrieval, adapters); infer pipeline orchestration in [`internal/inferexec`](internal/inferexec/)
- Documentation under `docs/` (architecture, observability, load testing, streaming, retrieval adapters, offline tooling, KB registry roadmap)

## v1.0.0: AI Request model, ledger, RAG, CLI

- **AI Request** — Optional fields on `POST /infer`: `request_type` (`inference` \| `rag` \| `agent`), `pipeline_ref` (defaults: `inference/basic:v1`, `rag/basic:v1`, `agent/basic:v0`), and `context` (RAG: `query`, `knowledge_base`; agent: tool hints for policy).
- **Request ledger** — Enable with `ledger.enabled` and `driver: memory`, `sqlite` (`path`), or `postgres` (`dsn`). Persists full normalized `AIRequest` as `ai_request_json` (plus `input`/`context` JSON), `policy_snapshot` after routing, and per-step records for audit and replay.
- **RAG (supported)** — Set `request_type: rag` and list `knowledge_bases` with `type: file` (local demo), **`http`** (JSON microservice), **`opensearch`** / **`elasticsearch`**, or **`meilisearch`**. See [`docs/retrieval-adapters.md`](docs/retrieval-adapters.md). Execution order: policy → overload admission → route → **retrieve** → **rerank** (`rag.rerank.type`, default `noop`) → chat completion. User text via `input.text` and/or `context.query`; chunks merge into `input.retrieved_context`.
- **Agent (request model only; no runtime)** — `request_type: agent` is accepted for **agent-ready** ingress and policy (tenant limits such as `max_steps`, `max_tool_calls`, `allowed_tools` when `features.agent_enabled` is true). There is **no** tool loop or agent executor in this release: the gateway returns **501** `agent_not_implemented`. Do not describe InferCore as a full “agent platform” yet.
- **CLI** (same binary):

```bash
infercore serve                    # HTTP gateway (default when no subcommand)
infercore trace <request_id>       # dump ledger request + steps JSON
infercore replay <request_id> --mode exact|current   # single response JSON
infercore replay id1 id2 --mode current              # batch: NDJSON lines (or --ids-file)
infercore ledger export-eval <request_id>... -o items.json   # eval dataset from ai_request_json
infercore eval run --dataset examples/queries.json --pipeline inference/basic:v1
# With server API key auth (or env INFERCORE_API_KEY):
infercore eval run --dataset examples/queries.json --api-key "$INFERCORE_API_KEY"
```

Replay and ledger commands use the **CLI** and [`internal/replay`](internal/replay/replay.go); the HTTP gateway does not expose a replay API (see [`docs/offline-tooling.md`](docs/offline-tooling.md)).

### Replay semantics (`infercore replay`)

- **`exact`** — Uses the **policy snapshot** in the ledger to force the **primary backend** and **disables fallback**. RAG requests still **re-run retrieve + rerank**; if knowledge-base files or retrieval config changed, results may differ from the original online call. Routing does not use live health probes in this mode.
- **`current`** — Re-evaluates **policy** and **router**. The replay harness treats **all backends as healthy** (see `internal/replay`); production `/infer` uses **cached health probes** for routing, so routes can differ from a `current` replay on the same machine.

### Telemetry

When `telemetry.tracing_enabled` is true, OTLP/log exporters receive **`infer_request`** (whole handler) and per-step **`infer_step`** spans (`step_type`, `backend`, `result`).

### Configuration checklists (minimal vs production)

Use this as a rough guide—not every deployment fits two buckets. Items reflect keys in `configs/infercore.example.yaml`.

| Area | Minimal (local demo / first run) | Production-oriented |
|------|----------------------------------|----------------------|
| **Server** | `server.host` / `server.port`; set `server.request_timeout_ms` to a value that covers your slowest expected path. | Tune `server.http.*` timeouts if you use long-lived connections; align `request_timeout_ms` with upstream SLAs and load balancer idle timeouts. |
| **Backends** | At least **one** backend in `backends` with a valid `type` (`mock` is enough to learn the control plane). | Real adapters (`vllm`, `openai_compatible`, …): correct `endpoint`, `timeout_ms`, `max_concurrency`, auth (`api_key` / headers), `health_path` for your provider. |
| **Routing** | `routing.default_backend` must name an entry in `backends`. Add `routing.rules` only when you need more than the default. | Explicit rules per tenant/task, and verify `default_backend` as safe last resort. |
| **Tenants** | Optional; policy still runs—ensure any `tenant_id` you send in JSON is consistent with how you intend to use policy. | Define `tenants` so quotas, classes, and limits match your product; keep tenant IDs stable across environments. |
| **Reliability** | Defaults (`reliability.overload`, `fallback_rules`) from the example are fine to start. | Model `fallback_rules` on real failure modes; choose `overload.action: reject` vs `degrade` for your capacity story; set `stream_fallback_enabled` deliberately. |
| **Telemetry** | `exporter: log` and metrics on is enough to debug. | OTLP (`otlp-http` / `otlp-http-json`) with `otlp_endpoint`, sensible batching; keep `metrics_enabled` / `tracing_enabled` aligned with observability cost. |
| **Ledger** | `ledger.enabled: false` or `driver: memory` for throwaway data. | `sqlite` with a persistent `path`, or `postgres` with `dsn`, for audit, `infercore trace` / `replay`, and multi-replica shared state. |
| **Ingress security** | Leave `infercore_api_key` unset for local use. | Set `infercore_api_key` (or `INFERCORE_API_KEY`); restrict network access; use the same key for `infercore eval run --api-key` / `INFERCORE_API_KEY`. |
| **RAG** | Omit `knowledge_bases` unless you call `request_type: rag`. | File-backed KB paths mounted in containers; document `context.knowledge_base` and query fields for clients. |
| **Health & SLO** | Defaults for `health_cache_ttl_ms` / SLO store are fine. | Tune health probe cadence vs routing churn; size `slo` store for your traffic if you rely on `/status` or metrics. |

**Minimal viable config (conceptually):** one backend + `routing.default_backend` pointing to it + valid YAML for telemetry exporter and reliability overload action. Everything else is layered behavior.

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

Infer (plain inference):

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

RAG (requires `knowledge_bases` in config, e.g. pointing at `examples/kb`):

```bash
curl -s -X POST http://localhost:8080/infer \
  -H "Content-Type: application/json" \
  -d '{
    "request_type": "rag",
    "tenant_id": "team-a",
    "task_type": "simple",
    "priority": "normal",
    "context": {"query": "What is InferCore?", "knowledge_base": "demo"},
    "input": {"text": "Answer using retrieved context."},
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

- `POST /infer` — unified JSON ingress for **inference**, **RAG**, and **agent (preview)** (`request_type` + `context` / `input` as documented above)
- `GET /health`
- `GET /status`
- `GET /metrics`

API draft: `api/openapi.yaml`

## Current runtime behavior

High-level summary of what the running service does today. For streaming details see [`docs/streaming-and-fallback.md`](docs/streaming-and-fallback.md); for metrics/logs see [`docs/observability.md`](docs/observability.md).

### Config & validation

- YAML loaded at startup (`INFERCORE_CONFIG`, or the default example path).
- Validates unique backend/tenant names, routing rule names, route/fallback backend references, and non-empty fallback triggers.
- Allowed fallback triggers: `timeout`, `backend_unhealthy`, `backend_error`.

### Request timeouts & HTTP server

- **`server.request_timeout_ms`** — wall-clock budget for each `/infer` after JSON body validation (**policy + routing + optional RAG retrieve/rerank + backend**). If this fires: **504** and `error.code=gateway_timeout`. If a per-backend **`timeout_ms`** fires first: **502** `execution_failed`.
- **`cmd/infercore`** sets `http.Server` `ReadTimeout` / `WriteTimeout` / `IdleTimeout` from `server.http.{read,write,idle}_timeout_ms` when set; otherwise derives from `request_timeout_ms` plus conventional slack (read / write / keep-alive).

### Tenant policy & routing

- **Policy:** reject unknown tenants, per-request budget gate (light estimate), per-tenant RPS limit (in-memory, 1s window), priority normalized from tenant defaults. **`priority`** may be omitted on `/infer`; it is filled from tenant config.
- **Routing:** rules by tenant class / task type / priority.
- **Health-aware routing:** skips backends failing `adapter.Health`, cached with `server.health_cache_ttl_ms` (same cache drives `/status` backend fields). If the default backend is unhealthy, uses the first healthy backend in config order (`healthy-fallback-order`). If none healthy: `route_error` on `/infer`.

### RAG

- **When:** `request_type: rag` on `POST /infer`.
- **Config:** `knowledge_bases[]` — `type` is one of **`file`** (`path`), **`http`** (`endpoint` POST URL), **`opensearch`** / **`elasticsearch`** (`endpoint` + `index`), **`meilisearch`** (`endpoint` + `index` + `api_key`). Optional: `api_key`, `headers`, `top_k`, `search_fields` (OpenSearch), `http_timeout_ms`. Optional `context.knowledge_base` selects the KB by name (otherwise the first listed KB). Set `rag.rerank.type` (v1.5: `noop` or future rerankers). Full contracts: [`docs/retrieval-adapters.md`](docs/retrieval-adapters.md).
- **Pipeline:** after routing, ledger steps **retrieve** and **rerank** run inside the same **`server.request_timeout_ms`** budget as the backend call. Retrieved chunks are merged into `input` (including `retrieved_context`) before the chat adapter runs.
- **Errors:** `400` `rag_not_configured` if KBs are missing or misconfigured; `400` `invalid_request` if the query text is empty (`input.text` / `context.query`). Retrieval/rerank failures surface as `502` `execution_failed` with the upstream error message.
- **Replay:** `infercore replay --mode exact` re-runs retrieve + rerank; changing KB files on disk can change results versus the original online call (see **Replay semantics** under v1.5 above).

### Overload, cost, reliability

- **Overload:** `reliability.overload.queue_limit` and `action` — `reject` → **503** `overload`; `degrade` → skip cost optimization and set `degrade` in the JSON. In-flight: `infercore_infer_inflight` on `/metrics`; `/status.queue_depth` matches the same counter.
- **Cost:** may pick a cheaper compatible backend within budget (healthy backends only).
- **Fallback:** timeout-aware chain from reliability config; structured event logs on policy rejection, execution failure, and fallback.

### `/infer` responses & errors

- Success includes **`trace_id`**; also **`policy_reason`** and **`effective_priority`** for debugging.
- Errors: `{ request_id, status, error: { code, message } }`.
- **`degrade`** appears when upstream streaming is degraded (see streaming doc).

### Backend adapters

| Kind | Config `type` | Notes |
|------|----------------|--------|
| Mock | `mock` | For tests / load profiles. |
| OpenAI-compatible | `vllm`, `openai`, `openai_compatible` | Same code path: **Chat Completions** + optional `GET` health. Supports `api_key` (default `Authorization: Bearer …`), optional `auth_header_name`, `headers`, **`health_path`** (default `/health`; many clouds use `/v1/models`), **`default_model`**. |
| Gemini (native) | `gemini` | `generateContent` / `streamGenerateContent`, API key. Example: [`configs/infercore.example.yaml`](configs/infercore.example.yaml). |
| Qwen (DashScope) | `openai_compatible` | OpenAI-compatible base, e.g. `https://dashscope.aliyuncs.com/compatible-mode/v1` with `health_path: /models`, `default_model`, `api_key` — **no** separate adapter. |

### Streaming (OpenAI-compatible)

- With **`options.stream=true`**, uses upstream SSE when supported; a JSON body instead is treated as **`stream_degraded`**. InferCore still returns **aggregated JSON** to the client.

### SLO, telemetry & metrics

- **SLO store** (in-memory, bounded by `slo.max_records` / `slo.max_age_ms`): TTFT, TPOT (when adapter provides it), completion latency, fallback markers.
- **Telemetry exporters:** `log`, `otlp-http-stub`, `otlp-http` (OTLP/HTTP protobuf), `otlp-http-json` (legacy JSON). Switches: `telemetry.metrics_enabled`, `telemetry.tracing_enabled`. OTLP: `otlp_flush_interval_ms`, `otlp_timeout_ms` (batching for `otlp-http`).
- Exporter emits metric/event logs per completed inference; trace hooks add trace/span IDs and result labels. When tracing is enabled, per-step **`infer_step`** records are emitted for pipeline steps (including `retrieve` / `rerank` for RAG).
- **`GET /status`:** exporter summary and **`scaling_signals`** (queue depth, timeout hint, rolling TTFT/fallback from SLO, `max_concurrency` hints).
- **`GET /metrics`:** Prometheus `client_golang` — e.g. `infercore_requests_total`, `infercore_infer_inflight`, `infercore_http_requests_total`, plus `infercore_scaling_*` gauges aligned with scaling signals.
- HTTP requests: `infercore_http_requests_total` with path/method/status labels.

### Optional API key & shutdown

- Gate with **`server.infercore_api_key`** or **`INFERCORE_API_KEY`**: send `X-InferCore-Api-Key` or `Authorization: Bearer …` on `/infer`, `/status`, `/metrics`. **`/health`** stays unauthenticated.
- On **SIGINT/SIGTERM**: graceful HTTP shutdown and OTLP flush (`Server.Shutdown`).

## Multi-instance deployment

Horizontal scale is typically **multiple InferCore replicas behind a load balancer**. Each process keeps its own in-memory policy windows, SLO store, overload counter, and health cache, so **global** RPS limits, concurrency caps, and rolling SLO ratios are **per replica** unless you add shared state (e.g. Redis) or aggregate in Prometheus. Use `/metrics` and `/status.scaling_signals` as inputs to cluster-level autoscaling.

## Project Structure

- `cmd/infercore`: service entrypoint and CLI (`trace`, `replay`, `ledger`, `eval`)
- `internal/server`: HTTP handlers (~1k-line `server.go` plus helpers); thin `infer` delegates to `internal/inferexec`
- `internal/inferexec`: infer execution pipeline (policy → route → RAG → backend) and phase plan helpers
- `internal/interfaces`: core module contracts
- `internal/types`: shared core data structures
- `configs`: YAML configuration examples
- `docs`: architecture, observability, retrieval adapters, offline tooling, KB roadmap
- `api`: OpenAPI contract draft

## Load testing

- Guide: `docs/load-testing.md` (throughput with [hey](https://github.com/rakyll/hey), overload / rate-limit / 504 behavior)
- Config: `configs/infercore.loadtest.yaml` (mock-only, high `queue_limit`, `rate_limit_rps: 0`)
- Script: `make load-infer` or `bash ./scripts/load-infer.sh` (env: `BASE_URL`, `DURATION`, `CONCURRENCY`, `QPS`, `INFERCORE_API_KEY`)

## Documents

- License: [`LICENSE`](LICENSE) (Apache-2.0)
- Architecture (one-pager for print/PDF): [`docs/architecture-one-pager.md`](docs/architecture-one-pager.md)
- Config example: [`configs/infercore.example.yaml`](configs/infercore.example.yaml)
- Observability: [`docs/observability.md`](docs/observability.md)
- Load testing: [`docs/load-testing.md`](docs/load-testing.md)
- Streaming & fallback: [`docs/streaming-and-fallback.md`](docs/streaming-and-fallback.md)
- Retrieval adapters (RAG): [`docs/retrieval-adapters.md`](docs/retrieval-adapters.md)
- Offline tooling (CLI vs HTTP gaps): [`docs/offline-tooling.md`](docs/offline-tooling.md)
- Extending retrieval adapters (developer guide): [`docs/extending-retrieval-adapters.md`](docs/extending-retrieval-adapters.md)
- API draft: [`api/openapi.yaml`](api/openapi.yaml)

## License

InferCore is licensed under the **Apache License, Version 2.0**. See the [`LICENSE`](LICENSE) file for the full text.

SPDX-License-Identifier: Apache-2.0

Third-party dependencies are subject to their own licenses (see module metadata and your `go.sum` / vendor tree as applicable).

## Next Implementation Steps

1. Optional Prometheus scrape endpoint; OTLP Logs for telemetry events.
2. Extend policy with quota windows and richer guardrail hooks.
3. Add benchmark scenarios and baseline-vs-infercore scripts.
4. Client-facing SSE from InferCore (passthrough/proxy streaming).
5. Add config versioning, hot reload, and migration tooling.
