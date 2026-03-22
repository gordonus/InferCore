# InferCore Architecture One-Pager

> **Note:** This document mirrors the architectural overview in [`README.md`](../README.md). Prefer editing **README** first, then aligning this file so PDF/print exports stay consistent.

## Project definition

InferCore is an open-source **AI Inference Control Plane**: a decision layer that sits **above** model serving and data plane systems.

It provides intelligent routing, cost-aware decisions, fallback and degrade orchestration, AI-native SLO signals, multi-tenant policy enforcement, and observability / scaling-signal exports.

**InferCore is not a model server.** It does not run token generation and does not replace vLLM, Triton, Ray Serve, or KServe.

**Website:** [infercore.dev](https://infercore.dev)

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
- **RAG orchestration** (optional): retrieval + rerank after routing, merge into model payload when `request_type: rag`
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

## High-level architecture

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
[ Optional RAG ] ← retrieve + rerank when request_type=rag and knowledge_bases configured
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

1. Client sends an AI request to InferCore (`POST /infer`) — inference, RAG, or agent (preview).
2. Gateway parses tenant, task type, priority, and payload.
3. Policy engine evaluates quota, budget, priority, and guardrails.
4. Overload admission may reject or degrade before routing.
5. Router selects a backend using rules, health, overload state, and optional cost optimization.
6. For **`request_type: rag`**, retrieval and rerank run (file-backed knowledge bases in v1.5), then retrieved text is merged into the payload before the model call.
7. Execution adapter invokes the selected backend.
8. On timeout or classified failure, reliability rules may trigger fallback or degrade behavior.
9. InferCore records TTFT/TPOT/latency/fallback (where available) and exports metrics, events, and traces as configured.

See the main [README](../README.md) for RAG configuration (`knowledge_bases`, `context.query`, ledger steps, and replay notes).

## Key differentiators

1. **Cost-aware routing** — Not only “can this run?” but “should this use the expensive model?” within budget and compatibility constraints.
2. **AI-native SLO** — TTFT, TPOT, completion latency, fallback markers; rolling hints exposed for operators (`/status`, Prometheus).
3. **Reliability orchestration** — Per-backend timeouts, configurable fallback chains, overload reject/degrade, health-aware routing.
4. **Multi-tenant policy** — Tenant classes, priorities, per-request budget estimates, per-tenant RPS windows (in-memory per replica).
5. **Scaling signals** — Queue depth, timeout-spike hints, TTFT degradation ratio, recent fallback rate, optional `max_concurrency` hints from config — intended for HPA/KEDA or custom autoscalers (**per-replica** unless you add shared state).

## Scaling signals (implementation)

The service exposes `scaling_signals` on `GET /status` and matching `infercore_scaling_*` Prometheus gauges (queue depth, timeout spike hint, rolling TTFT/fallback aggregates, optional `max_concurrency` hints). These are intended for HPA/KEDA or custom autoscalers; they reflect **per-replica** in-memory state unless extended with shared stores. See [observability.md](./observability.md) for metric and status field details.

## Further reading

- [infercore.dev](https://infercore.dev) — official website (overview, blog)
- [README.md](../README.md) — full project quickstart, runtime behavior, and document index
- [backend-adapters.md](./backend-adapters.md) — backend types and configuration
- [observability.md](./observability.md) — metrics, status, timeouts
- [LICENSE](../LICENSE) — Apache License 2.0
