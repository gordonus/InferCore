# InferCore sample knowledge base

This directory can be referenced from InferCore config as a **file-backed** knowledge base for **RAG** (`request_type: rag` on `POST /infer`).

## Behaviour

- Text files are split into chunks on blank lines; retrieval uses simple token-overlap scoring over those chunks.
- In `configs/infercore.example.yaml`, the demo KB is named `demo` and points at `examples/kb`. Clients can set `context.knowledge_base` to that name (or rely on the first configured KB).

## Related docs

- Website: [infercore.dev](https://infercore.dev)
- Main guide: [`README.md`](../../README.md) (v1.5 section, **RAG** under *Current runtime behavior*).
- OpenAPI: [`api/openapi.yaml`](../../api/openapi.yaml).
