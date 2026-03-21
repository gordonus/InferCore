# Retrieval adapters (RAG)

InferCore wires `knowledge_bases` from YAML into `RetrievalAdapter` ([`internal/interfaces/interfaces.go`](../internal/interfaces/interfaces.go)). Each KB has a `name` selected by clients via `context.knowledge_base` (or the first KB in config).

## Built-in types

| `type` | Purpose | Required YAML |
|--------|---------|----------------|
| `file` | Local `.md`/`.txt` demo index | `path` |
| `http` | Generic JSON retrieval microservice | `endpoint` (POST URL) |
| `opensearch` / `elasticsearch` | Lexical `multi_match` on an index | `endpoint` (cluster base URL), `index` |
| `meilisearch` | Hosted Meilisearch search | `endpoint`, `index` (UID), `api_key` |

Optional for all HTTP-backed types: `api_key`, `headers`, `top_k`, `http_timeout_ms`.

### `http`

- **Request** (JSON): `{"query":"...","top_k":N,"kb":"<name>"}`  
- **Response** (JSON): `{"chunks":[{"text":"...","source":"...","score":0}]}`
- Same shape under `results` is accepted.
- Use `headers` for custom auth, e.g. `Authorization: Bearer ...` (overrides `api_key`).

### `opensearch` / `elasticsearch`

- **Request**: `POST {endpoint}/{index}/_search` with `multi_match` over `search_fields` (default `text`, `content`, `body`).
- **Response**: standard `hits.hits[]`; `_source` must include at least one of those fields (or `passage`, `chunk`).
- **Auth**: set `headers.Authorization` for full control, or `api_key` as raw Elastic API key (`ApiKey <key>` is applied), or prefix value with `Basic ` / `Bearer ` / `ApiKey `.

### `meilisearch`

- **Request**: `GET {endpoint}/indexes/{index}/search?q=...&limit=N`
- **Auth**: `Authorization: Bearer <api_key>` unless overridden in `headers`.
- **Hits**: document fields at top level; text read from `text`, `content`, `body`, or `passage` (or `_formatted`).

## Pinecone / pure vector DBs

Text-in retrieval against Pinecone (and similar) usually requires an **embedding step** first. Those are not implemented here; use an `http` adapter in front of your own embedding + vector query service, or extend `RetrievalAdapter` in-tree.

## `opts` map

`Retrieve(ctx, query, opts)` may receive `top_k` in `opts` (from future request/context wiring); it overrides the YAML `top_k` when set.

## Implementing a new adapter

See [`extending-retrieval-adapters.md`](./extending-retrieval-adapters.md) for wiring (`FromConfig`, config validation, tests) and conventions.

## See also

- **Website:** [infercore.dev](https://infercore.dev) — product overview and blog
- [README.md](../README.md) — project index
