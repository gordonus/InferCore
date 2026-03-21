# Extending `RetrievalAdapter`

This guide explains how to add or change **RAG retrieval backends** in InferCore. For **YAML contracts and wire formats** of the built-in types (`file`, `http`, OpenSearch, Meilisearch), see [`retrieval-adapters.md`](./retrieval-adapters.md).

## What you implement

[`RetrievalAdapter`](../internal/interfaces/interfaces.go) is minimal on purpose:

```go
type RetrievalAdapter interface {
	Name() string
	Retrieve(ctx context.Context, query string, opts map[string]any) (types.RetrievalResult, error)
}
```

- **`Name()`** must match the `name` field of the corresponding `knowledge_bases` entry in YAML (used as the map key in `FromConfig`).
- **`Retrieve`** returns [`types.RetrievalResult`](../internal/types/types.go): a slice of [`RetrievalChunk`](../internal/types/types.go) with at least `Text` set; `Source`, `Score`, and `Extra` are optional but useful for debugging and clients.

## Where adapters are built and used

```mermaid
flowchart LR
  YAML[knowledge_bases YAML]
  Validate[config.Validate]
  FromConfig[retrieval.FromConfig]
  Map["map[name]RetrievalAdapter"]
  RAG[infer_rag / replay]
  YAML --> Validate
  Validate --> FromConfig
  FromConfig --> Map
  Map --> RAG
```

- **Construction:** [`internal/retrieval/build.go`](../internal/retrieval/build.go) — `FromConfig` loops `cfg.KnowledgeBases`, lowercases `type`, and dispatches to `New…` constructors. Unknown types log `retrieval_init_skipped` and omit that KB from the map.
- **Selection at runtime:** [`internal/server/infer_rag.go`](../internal/server/infer_rag.go) picks the adapter by `context.knowledge_base`, or the **first** configured KB if unset.
- **Replay:** [`internal/replay/replay.go`](../internal/replay/replay.go) calls the same retrieval path with `opts` currently **`nil`** (see below).

## Step-by-step: add a new `type`

1. **Implement the adapter** under [`internal/retrieval/`](../internal/retrieval/) (e.g. `myengine_kb.go`). Typical pattern:
   - Constructor `NewMyEngineKB(kb config.KnowledgeBaseConfig) (*MyEngineKB, error)` validates required fields and stores config + `http.Client` if outbound HTTP is used.
   - `Retrieve` respects `ctx` cancellation, maps errors clearly, and fills `RetrievalResult.Chunks`.

2. **Reuse helpers** in [`common.go`](../internal/retrieval/common.go):
   - `effectiveTopK(kb, opts, fallback)` — honors `opts["top_k"]` (int / int64 / float64) over YAML `top_k`.
   - `httpClientTimeoutMS(kb)` — uses `http_timeout_ms` or defaults to 30s.

3. **Register in `FromConfig`** — add a `case` in [`build.go`](../internal/retrieval/build.go) matching the YAML `type` string (use `strings.ToLower` consistently). On constructor error, log and `continue` like existing cases.

4. **Validate config** — extend the `knowledge_bases` loop in [`internal/config/config.go`](../internal/config/config.go) `(*Config).Validate`:
   - Add your `case` with required fields for the new type.
   - Extend the `default` error message to list the new type so mis-typed configs fail fast at startup.

5. **Optional: extend `KnowledgeBaseConfig`** — add new YAML fields with clear comments in the same file. Prefer reusing `endpoint`, `headers`, `api_key`, `top_k`, `http_timeout_ms` when they fit.

6. **Document for operators** — update [`retrieval-adapters.md`](./retrieval-adapters.md) (table + section) and [`configs/infercore.example.yaml`](../configs/infercore.example.yaml) with a commented example.

7. **Tests** — table-driven or `httptest.Server` tests (see [`http_kb_test.go`](../internal/retrieval/http_kb_test.go)) for JSON parsing, timeouts, and error paths.

## Auth and headers (HTTP-backed adapters)

Built-in HTTP adapters apply **`headers` first**, then set `Authorization` from `api_key` only if it is still empty—so operators can supply `Bearer`, `Basic`, `ApiKey`, or custom schemes in YAML. When adding a new adapter, follow the same order to avoid surprising overrides.

## The `opts` map

Today, production call sites pass **`nil`** for `opts`. `effectiveTopK` already supports `opts["top_k"]` for forward compatibility. If you introduce new keys, document them here and in `retrieval-adapters.md` once wired from the API or replay.

## Vector-only databases (Pinecone, etc.)

Pure vector search usually needs **embeddings** before query. InferCore does not embed in-process for those engines. Practical options: expose a small **HTTP** retrieval service and use `type: http`, or implement a dedicated adapter that performs embed + search if you accept the extra dependencies and config surface.

---

## Appendix: roadmap ideas (not implemented)

The following were sketched for a future **registry / versioned retrieval** layer; they are **not** in the current `RetrievalAdapter` contract:

- Published **`index_version`** (or content hash) per logical KB for audit and replay pinning.
- **`RetrieveOptions`** (structured filters, namespace, version override) instead of only `map[string]any`.
- Ledger fields capturing retrieval policy snapshot for `replay --mode exact`.

Keep the adapter interface stable; prefer optional structs or extended `opts` keys when those features land.
