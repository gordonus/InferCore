# infercore-client (Python)

Minimal client for the InferCore gateway. Only **`POST /infer`** is used for inference and RAG.

## Install

```bash
pip install .
# or with a recent pip: pip install -e .
```

Run tests (stdlib only):

```bash
cd sdk/python && PYTHONPATH=. python3 -m unittest discover -s tests -v
```

## Auth

If the server requires an API key (`server.infercore_api_key` or `INFERCORE_API_KEY`):

```python
InferCoreClient("http://localhost:8080", api_key="secret", auth="header")   # X-InferCore-Api-Key
InferCoreClient("http://localhost:8080", api_key="secret", auth="bearer")    # Authorization: Bearer
```

## Example

```python
from infercore import InferCoreClient

c = InferCoreClient("http://127.0.0.1:8080", api_key="...", auth="header")
r = c.infer({
    "tenant_id": "demo",
    "task_type": "chat",
    "priority": "normal",
    "input": {"text": "Hello"},
    "options": {"stream": False, "max_tokens": 512},
})
print(r["result"]["text"])
```

See **[docs/langchain-integration.md](../../docs/langchain-integration.md)** for RAG (`request_type: rag`, `context.knowledge_base`) and LangChain integration notes.
