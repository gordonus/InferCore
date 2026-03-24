"""Tests for InferCoreClient (auth headers and POST /infer)."""

import unittest
from unittest.mock import MagicMock

import requests

from infercore import InferCoreClient


class TestInferCoreClient(unittest.TestCase):
    def test_auth_header_mode_sets_x_infercore_api_key(self):
        s = requests.Session()
        InferCoreClient("http://localhost:8080", api_key="k", auth="header", session=s)
        self.assertEqual(s.headers["X-InferCore-Api-Key"], "k")

    def test_auth_bearer_mode_sets_authorization(self):
        s = requests.Session()
        InferCoreClient("http://localhost:8080", api_key="tok", auth="bearer", session=s)
        self.assertEqual(s.headers["Authorization"], "Bearer tok")

    def test_infer_raises_for_status_on_error(self):
        s = MagicMock()
        mock_resp = MagicMock()
        mock_resp.raise_for_status.side_effect = requests.HTTPError()
        s.post.return_value = mock_resp
        c = InferCoreClient("http://localhost:8080", session=s)
        body = {
            "tenant_id": "t",
            "task_type": "x",
            "priority": "p",
            "input": {},
            "options": {"stream": False, "max_tokens": 8},
        }
        with self.assertRaises(requests.HTTPError):
            c.infer(body)


if __name__ == "__main__":
    unittest.main()
