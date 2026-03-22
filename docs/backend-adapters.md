# Backend adapters

This document describes which **inference backends** InferCore supports, how they differ on the wire, and a practical order for extending adapters.

InferCore is a **control plane**, not a universal multi-vendor SDK. Vendors that expose an **OpenAI-compatible HTTP** API can often be used with `openai_compatible` (or `vllm`) plus `endpoint` and credentials, without a new adapter package.

## What InferCore implements today

Backend types are registered in [`internal/adapters/factory.go`](../internal/adapters/factory.go).

| `backends[].type` | Package | Protocol |
|-------------------|---------|----------|
| `mock` | `internal/adapters/mock` | In-process test double |
| `vllm`, `openai`, `openai_compatible` | `internal/adapters/vllm` | **OpenAI Chat Completions** (`POST /v1/chat/completions`), JSON or SSE stream |
| `azure_openai` | `internal/adapters/vllm` | Same client as OAI; **Azure** base URL, `api-version`, deployment in `default_model`, default header `api-key` |
| `anthropic` | `internal/adapters/anthropic` | **Anthropic Messages API** (`POST /v1/messages`), JSON or SSE stream |
| `bedrock` | `internal/adapters/bedrock` | **AWS Bedrock** `Converse` (AWS SDK v2, SigV4 via default credential chain) |
| `gemini` | `internal/adapters/gemini` | **Google AI Studio** Gemini `generateContent` / `streamGenerateContent` (API key: `x-goog-api-key`) |
| `gemini_vertex` | `internal/adapters/gemini` | **Vertex AI** Gemini: regional `aiplatform.googleapis.com` URLs, `Authorization: Bearer` (typically an OAuth access token) |

Config validation for these types lives in [`internal/config/config.go`](../internal/config/config.go).

The adapter contract is [`BackendAdapter`](../internal/interfaces/interfaces.go): `Invoke`, `Health`, `Metadata`.

### Minimal YAML examples

**Azure OpenAI** — `endpoint` is the Azure resource base (no `/openai` suffix required in path before we append routes). `default_model` is the **deployment name**. `api_version` defaults to `2024-02-15-preview` if unset.

```yaml
backends:
  - name: azure-gpt4
    type: azure_openai
    endpoint: https://YOUR_RESOURCE.openai.azure.com
    api_key: ${AZURE_OPENAI_KEY}
    default_model: my-deployment-name
    # api_version: 2024-02-15-preview
    # auth_header_name: api-key   # default; override if your setup uses a different header
```

**Anthropic** — optional `endpoint` (default `https://api.anthropic.com`). Health checks `GET {endpoint}{health_path}`; default `health_path` is `/v1/models` (override if your environment uses a different probe).

```yaml
backends:
  - name: claude
    type: anthropic
    api_key: ${ANTHROPIC_API_KEY}
    default_model: claude-3-5-sonnet-20241022
```

**AWS Bedrock** — uses the **default AWS credential chain** (environment variables, shared config, IAM role, etc.). `default_model` is the Bedrock **model ID** (e.g. `anthropic.claude-3-5-sonnet-20240620-v1:0`). Streaming is not implemented yet in this adapter.

```yaml
backends:
  - name: bedrock-claude
    type: bedrock
    aws_region: us-east-1
    default_model: anthropic.claude-3-5-sonnet-20240620-v1:0
```

**Gemini (AI Studio)**

```yaml
backends:
  - name: gemini-flash
    type: gemini
    # endpoint omitted → https://generativelanguage.googleapis.com
    api_key: ${GEMINI_API_KEY}
    default_model: gemini-2.0-flash
```

**Vertex AI (Gemini)** — `api_key` is sent as **`Bearer`**. In practice this is often a short-lived OAuth access token (`gcloud auth print-access-token`) or a token from your identity stack—not the AI Studio API key. Optional `endpoint` overrides the default host `https://{vertex_location}-aiplatform.googleapis.com` (e.g. for proxies).

```yaml
backends:
  - name: vertex-gemini
    type: gemini_vertex
    vertex_project: my-gcp-project
    vertex_location: us-central1
    api_key: ${VERTEX_ACCESS_TOKEN}
    default_model: gemini-2.0-flash
```

## Choosing a backend type (by wire protocol)

| Bucket | Meaning | Typical approach in InferCore |
|--------|---------|-------------------------------|
| **OpenAI-compatible HTTP** | Speaks Chat Completions (or sits behind a proxy that does) | `openai_compatible` (or `vllm`) + `endpoint` + `api_key` / `headers` |
| **Native non-OAI** | Custom JSON/RPC (e.g. Anthropic Messages, Bedrock, Gemini REST) | Dedicated `type` + package (`anthropic`, `bedrock`, `gemini`, …) |
| **Cloud auth + OAI path** | Same Chat Completions payloads but special base URL, versions, or headers | `azure_openai` or `openai_compatible` with `headers` / `auth_header_name` |

## Suggested priority for **new native** adapters

When choosing what to implement in-tree (vs using an OpenAI-compatible gateway in front of a vendor), a practical order is:

1. **Streaming for Bedrock** — parity with other backends when policy requires native Bedrock.
2. **Other Bedrock invoke paths** — only if `Converse` is insufficient for target models.
3. **Other vendors** — only if there is no stable OAI surface and maintenance cost is justified.

Core Anthropic, Azure OpenAI, Bedrock (non-stream), and Gemini / Vertex are already in-tree as above.

## Composition pattern (recommended)

Many teams deploy:

```text
Clients → InferCore (policy, routing, RAG, metrics) → OpenAI-compatible gateway or vendor fan-out → models
```

InferCore focuses on **tenant policy, reliability, and observability**; a separate compatibility layer can cover **provider breadth** when you do not want every vendor protocol inside the control plane.

## See also

- [README.md](../README.md) — backends and execution layer overview
- [architecture-one-pager.md](./architecture-one-pager.md) — one-page architecture
- [streaming-and-fallback.md](./streaming-and-fallback.md) — streaming and adapter behavior
