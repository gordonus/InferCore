"""HTTP client for InferCore gateway: single ingress POST /infer."""

from __future__ import annotations

from typing import Any, Dict, Literal, Optional

import requests


AuthMode = Literal["header", "bearer"]


class InferCoreClient:
    """
    Calls InferCore POST /infer with AIRequest JSON.

    Authentication (when server has infercore_api_key / INFERCORE_API_KEY set):
      - auth=\"header\" -> X-InferCore-Api-Key (same as eval CLI)
      - auth=\"bearer\" -> Authorization: Bearer <api_key>
    """

    def __init__(
        self,
        base_url: str,
        api_key: Optional[str] = None,
        *,
        auth: AuthMode = "header",
        timeout: float = 120.0,
        session: Optional[requests.Session] = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self._timeout = timeout
        self._session = session or requests.Session()
        if api_key:
            if auth == "header":
                self._session.headers["X-InferCore-Api-Key"] = api_key
            elif auth == "bearer":
                self._session.headers["Authorization"] = f"Bearer {api_key}"
            else:
                raise ValueError("auth must be 'header' or 'bearer'")

    def infer(self, body: Dict[str, Any]) -> Dict[str, Any]:
        """
        POST /infer. Body must satisfy AIRequest (tenant_id, task_type, input, options.max_tokens > 0, ...).

        Returns parsed JSON; on 2xx success this matches AIResponse (result, request_id, ...).
        """
        url = f"{self.base_url}/infer"
        resp = self._session.post(
            url,
            json=body,
            timeout=self._timeout,
            headers={"Content-Type": "application/json"},
        )
        resp.raise_for_status()
        data: Dict[str, Any] = resp.json()
        return data

    def health(self) -> Dict[str, Any]:
        """GET /health (unauthenticated)."""
        resp = self._session.get(f"{self.base_url}/health", timeout=min(self._timeout, 30.0))
        resp.raise_for_status()
        return resp.json()
