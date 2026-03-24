# LangChain and other application frameworks

**Positioning:** LangChain (or similar) is the **application framework**—prompts, chains, agents, and app logic. InferCore is the **runtime control plane**—unified ingress, routing, policy, RAG orchestration (when configured), reliability, ledger, and observability.

The HTTP gateway exposes a **single AI ingress**: **`POST /infer`**. There is no separate `/rag/query` route; RAG uses the same endpoint with `request_type: rag` and RAG-specific fields in `context` / `input`. See [`api/openapi.yaml`](../api/openapi.yaml) and [`internal/types/types.go`](../internal/types/types.go).

## Contract summary (aligned with the server)

### Request: `AIRequest` on `POST /infer`

| Field | Notes |
|-------|--------|
| `tenant_id` | Required. |
| `task_type` | Required. |
| `priority` | Required by schema; use your tenant/routing convention. |
| `input` | Required map; for chat-style calls often `{"text": "..."}`. |
| `options` | Required; **`max_tokens` must be greater than 0**; set `stream` as needed. |
| `request_type` | Omit or `inference` for plain inference; **`rag`** for retrieve + rerank + model. |
| `pipeline_ref` | Optional; defaults include `inference/basic:v1`, `rag/basic:v1`. |
| `context` | For RAG: **`knowledge_base`** (name of a configured KB), optional **`query`** (else `input.text` is used). |

RAG KB selection uses **`context.knowledge_base`**, not `kb_ref`. If omitted and `knowledge_bases` exist in config, the server may fall back to the **first** configured KB.

### Response: `AIResponse`

On success, JSON includes **`result`** (object, adapter output; typically includes **`text`** for chat), plus `request_id`, `selected_backend`, `route_reason`, `metrics`, `fallback`, `degrade`, etc. Do **not** assume a top-level `output` field—use **`result`** per [`types.AIResponse`](../internal/types/types.go).

### Authentication

When `server.infercore_api_key` or `INFERCORE_API_KEY` is set, send either:

- **`X-InferCore-Api-Key: &lt;key&gt;`**, or  
- **`Authorization: Bearer &lt;key&gt;`**

`/health` remains unauthenticated. See [`internal/server/server.go`](../internal/server/server.go) (`withOptionalInfercoreAuth`).

## Integration modes

1. **Model path (thin)** — Wrap `POST /infer` with `request_type` inference (or omit). LangChain keeps local RAG; only the **model call** goes through InferCore. Replay of retrieval is not captured by InferCore.
2. **RAG path (full control plane)** — Use **`request_type: rag`** so InferCore runs retrieve → rerank → model. Policy, ledger, and traces cover the full path.
3. **Agent / LangGraph (later)** — Same `POST /infer`; `request_type: agent` is limited today—check server behavior and [`openapi.yaml`](../api/openapi.yaml) before relying on it.

## Python client

A minimal client lives under **[`sdk/python/`](../sdk/python/)** (`infercore` package). It only speaks **`POST /infer`** and supports both API-key header styles.

```bash
cd sdk/python && pip install -e .
```

Usage sketch:

```python
from infercore import InferCoreClient

client = InferCoreClient("http://localhost:8080", api_key="secret", auth="header")
# or: auth="bearer"

out = client.infer({
    "tenant_id": "t1",
    "task_type": "chat",
    "priority": "normal",
    "input": {"text": "Hello"},
    "options": {"stream": False, "max_tokens": 1024},
})
text = out["result"]["text"]
```

RAG:

```python
out = client.infer({
    "request_type": "rag",
    "pipeline_ref": "rag/basic:v1",
    "tenant_id": "t1",
    "task_type": "chat",
    "priority": "normal",
    "context": {"knowledge_base": "product_docs", "query": "How does fallback work?"},
    "input": {"text": "How does fallback work?"},
    "options": {"stream": False, "max_tokens": 1024},
})
text = out["result"]["text"]
```

## LangChain wrappers (patterns)

These are **patterns**, not shipped dependencies. Implement with `langchain_core` in your app if desired.

- **Chat model** — Subclass `BaseChatModel`; in `_generate`, map messages to `input` / `context`, call `client.infer(...)`, read **`result["text"]`** into `AIMessage`.
- **RAG runnable** — A small `Runnable` or tool that builds the **`rag`** body above and returns the same `result`.

Keep a single contract: **`POST /infer`** + `AIRequest` / `AIResponse`.

## Replay and request lookup (current limits)

- **Replay** is implemented as **`infercore replay`** (CLI), not as an HTTP route on the gateway. See [`cmd/infercore`](../cmd/infercore/main.go). To replay from automation, invoke the CLI or add a future HTTP API in the server (product decision).
- **GET `/requests/{id}`** is **not** exposed on the HTTP gateway today; the request **ledger** is used internally and by offline tools. Use returned **`request_id`** from `POST /infer` for correlation in your own stores, or extend the server if you need REST lookup.

## Eval

Batch eval in this repo posts each dataset row to **`POST /infer`** (see [`internal/eval/eval.go`](../internal/eval/eval.go)). There is no separate `/eval/run` HTTP endpoint.

## See also

- [README.md](../README.md) — overview and request lifecycle  
- [api/openapi.yaml](../api/openapi.yaml) — `AIRequest` / `AIResponse` schemas  
- [docs/retrieval-adapters.md](./retrieval-adapters.md) — knowledge bases for RAG  
