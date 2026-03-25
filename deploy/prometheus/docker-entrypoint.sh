#!/bin/sh
# Generate /tmp/prometheus.yml from env (Grafana Cloud remote_write does not reliably
# expand ${VAR} in prometheus.yml even with --enable-feature=expand-environment-variables).
set -eu
TEMPLATE="/etc/prometheus/prometheus.template.yml"
OUT="/tmp/prometheus.yml"

if [ ! -f "$TEMPLATE" ]; then
	echo "error: missing $TEMPLATE" >&2
	exit 1
fi
if [ -z "${GC_REMOTE_WRITE_URL:-}" ] || [ -z "${GC_REMOTE_WRITE_USER:-}" ] || [ -z "${GC_REMOTE_WRITE_PASSWORD:-}" ]; then
	echo "error: set GC_REMOTE_WRITE_URL, GC_REMOTE_WRITE_USER, GC_REMOTE_WRITE_PASSWORD in .env" >&2
	exit 1
fi

awk -v u="$GC_REMOTE_WRITE_URL" \
	-v user="$GC_REMOTE_WRITE_USER" \
	-v pass="$GC_REMOTE_WRITE_PASSWORD" '
{
	gsub(/__REMOTE_WRITE_URL__/, u)
	gsub(/__REMOTE_WRITE_USER__/, user)
	gsub(/__REMOTE_WRITE_PASSWORD__/, pass)
	print
}' "$TEMPLATE" >"$OUT"

# Ignore image default CMD (--config.file=...) so we only use generated config.
exec /bin/prometheus \
	--config.file="$OUT" \
	--storage.tsdb.path=/prometheus \
	--web.enable-lifecycle
