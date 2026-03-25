# Local Prometheus → Grafana Cloud (remote_write)

Scrape InferCore’s `GET /metrics` and forward series to **Grafana Cloud Metrics** (Mimir-compatible remote write).

## Prerequisites

### InferCore

1. Enable Prometheus text metrics:

   ```yaml
   telemetry:
     metrics_enabled: true
   ```

   See [configs/infercore.example.yaml](../../configs/infercore.example.yaml).

2. Confirm metrics locally (no API key):

   ```bash
   curl -sS http://127.0.0.1:8080/metrics | head
   ```

   If `server.infercore_api_key` (or `INFERCORE_API_KEY`) is set, only `/health` is anonymous. Use:

   ```bash
   curl -sS -H "Authorization: Bearer YOUR_KEY" http://127.0.0.1:8080/metrics | head
   ```

   For Prometheus, edit `prometheus.template.yml`: uncomment the `authorization` block and set the Bearer token (see template comments).

### Grafana Cloud

1. Open your Grafana Cloud stack → **Metrics** (or **Send metrics** / Hosted Prometheus).
2. Copy **remote write URL** (often `https://prometheus-prod-….grafana.net/api/prom/push`).
3. Copy **username** (often numeric instance / user id) and **password** / API token for remote write.

## Setup

Remote write credentials come from **`.env`** only. The container entrypoint (`docker-entrypoint.sh`) substitutes `__REMOTE_WRITE_*__` placeholders in `prometheus.template.yml` into `/tmp/prometheus.yml` using **`awk`**. This avoids relying on Prometheus’s `--enable-feature=expand-environment-variables`, which often **does not** expand variables inside `remote_write.url` (you would otherwise see errors like `unsupported protocol scheme ""` and a URL of `${GC_REMOTE_WRITE_URL}`).

```bash
cd deploy/prometheus
cp .env.example .env
# Edit .env: GC_REMOTE_WRITE_URL, GC_REMOTE_WRITE_USER, GC_REMOTE_WRITE_PASSWORD
# Optional: edit prometheus.template.yml (scrape port host.docker.internal:8081, Bearer auth, …)
docker compose up -d
```

- **Scrape target** `host.docker.internal:8080` reaches InferCore on the **host** from the Prometheus container (Mac/Windows/Linux with Compose `host-gateway`).

## Verification

From this directory, a quick file / optional connectivity check (no credentials required):

```bash
./verify-local.sh
```

1. **Prometheus targets**  
   Open http://localhost:9090/targets — job `infercore` should be **UP** (InferCore must be running on the host).

2. **Prometheus has samples**  
   In http://localhost:9090/graph try `infercore_requests_total` or `infercore_infer_inflight`.

3. **Grafana Cloud**  
   In Grafana Cloud **Explore**, select your Prometheus/Mimir data source and query the same metric names (allow a short delay after remote_write).

4. **Remote write errors**  
   Check container logs: `docker compose logs -f prometheus` for 401/403 (wrong credentials) or DNS issues.

### Troubleshooting: `unsupported protocol scheme ""` / URL shows `${GC_REMOTE_WRITE_…}`

That means the remote write URL was never substituted. Use the current layout (**`prometheus.template.yml` + `docker-entrypoint.sh` + `.env`**) and restart: `docker compose up -d --force-recreate`. Do not put raw `${VAR}` in YAML expecting Prometheus to expand it for `remote_write`.

## Do not double-publish metrics

If InferCore uses `telemetry.exporter: otlp-http` to send metrics elsewhere, avoid also scraping `/metrics` to the same backend, or you will duplicate series.

## Shutdown

```bash
docker compose down
```

To remove TSDB data: `docker compose down -v`.
