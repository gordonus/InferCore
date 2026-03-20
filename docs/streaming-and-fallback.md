# Streaming and fallback

## End-to-end streaming (OpenAI-compatible adapter)

When `options.stream: true` on `POST /infer`:

1. The OpenAI-compatible adapter (`vllm` / `openai` / `openai_compatible`) calls `POST /v1/chat/completions` with `"stream": true` and `Accept: text/event-stream`.
2. If the upstream responds with `text/event-stream`, InferCore reads SSE `data:` lines, parses JSON chunks, concatenates `choices[0].delta.content`, and measures **TTFT** from invoke start to the first non-empty delta.
3. If the upstream returns JSON (non-stream) instead, the adapter treats this as **degraded** streaming: `stream_degraded: true` in the result payload, and timing is non-stream (full response latency).

The HTTP response from InferCore remains **JSON** (aggregated `result.text`), not an SSE stream to the client. True passthrough SSE to clients is future work.

## Fallback and streaming

`reliability.stream_fallback_enabled` (default **false**):

- **false**: after the first backend fails and a fallback backend is tried, the request is sent with `stream: false` so fallback paths avoid partial stream bodies and provider-specific stream semantics.
- **true**: fallback backends receive the same `stream` flag as the primary (use only if all backends support compatible streaming).

Primary failures that match configured fallback triggers still use the reliability fallback chain; only the stream flag behavior changes.

## RAG and streaming

For `request_type: rag`, **retrieve** and **rerank** run **before** the chat completion call. They are non-streaming steps; only the final model invocation honors `options.stream` and the streaming behavior described above. The gateway timeout (`server.request_timeout_ms`) applies to the whole chain (policy + route + retrieve + rerank + backend).
