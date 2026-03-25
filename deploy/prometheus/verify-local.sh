#!/usr/bin/env bash
# Lightweight checks for the Prometheus bundle (no Docker required for file checks).
set -euo pipefail
ROOT=$(cd "$(dirname "$0")" && pwd)
for f in prometheus.template.yml docker-entrypoint.sh docker-compose.yml .env.example README.md; do
  test -f "$ROOT/$f" || { echo "missing $f"; exit 1; }
done
echo "OK: deploy/prometheus bundle files present"

if curl -sf --max-time 2 "http://127.0.0.1:8080/metrics" | head -1 | grep -q .; then
  echo "OK: InferCore /metrics reachable on :8080"
else
  echo "SKIP: InferCore not reachable on :8080 (start infercore with telemetry.metrics_enabled=true)"
fi

if curl -sf --max-time 2 "http://127.0.0.1:9090/-/ready" >/dev/null; then
  echo "OK: Prometheus ready on :9090"
else
  echo "SKIP: Prometheus not running on :9090 (run: cp .env.example .env && docker compose up -d)"
fi
