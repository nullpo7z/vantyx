#!/usr/bin/env bash
# OWASP ZAP full scan (spider + active) with session cookie and SPA seed URLs.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
REPORT_DIR="${ZAP_REPORT_DIR:-$ROOT/zap-reports}"
TARGET="${ZAP_TARGET:-https://127.0.0.1}"
LOGIN_TARGET="${VANTYX_LOGIN_TARGET:-$TARGET}"
SPIDER_MINS="${ZAP_SPIDER_MINS:-30}"

if [[ -z "${VANTYX_ZAP_SESSION_COOKIE:-}" ]]; then
  eval "$("$ROOT/scripts/zap/login.sh" "$LOGIN_TARGET")"
fi

mkdir -p "$REPORT_DIR"

DOCKER_NET=()
if [[ "${ZAP_USE_HOST_NETWORK:-1}" == "1" ]]; then
  DOCKER_NET=(--network host)
else
  DOCKER_NET=(--add-host=host.docker.internal:host-gateway)
fi

docker run --rm "${DOCKER_NET[@]}" \
  -e VANTYX_ZAP_SESSION_COOKIE \
  -e VANTYX_ZAP_SEED_PATHS \
  -v "$REPORT_DIR:/zap/wrk/:rw" \
  -v "$ROOT/scripts/zap/zap-rules.conf:/zap/wrk/zap-rules.conf:ro" \
  -v "$ROOT/scripts/zap/hooks.py:/zap/wrk/hooks.py:ro" \
  -t ghcr.io/zaproxy/zaproxy:stable \
  zap-full-scan.py \
  -t "$TARGET" \
  -c /zap/wrk/zap-rules.conf \
  --hook=/zap/wrk/hooks.py \
  -z "-config certificate.ignore=true" \
  -z "-config spider.ajax.enable=true" \
  -r zap-full-auth-report.html \
  -J zap-full-auth-report.json \
  -w zap-full-auth-report.md \
  -m "$SPIDER_MINS" \
  -I
