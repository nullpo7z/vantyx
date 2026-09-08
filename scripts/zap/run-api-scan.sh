#!/usr/bin/env bash
# OWASP ZAP OpenAPI scan (safe mode by default; set ZAP_API_ACTIVE=1 for active rules).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
REPORT_DIR="${ZAP_REPORT_DIR:-$ROOT/zap-reports}"
TARGET="${ZAP_TARGET:-https://127.0.0.1}"
LOGIN_TARGET="${VANTYX_LOGIN_TARGET:-$TARGET}"
# Full assessment: ZAP_API_ACTIVE=1 (omit -S). Default remains safe for CI.
SAFE_FLAG="-S"
if [[ "${ZAP_API_ACTIVE:-0}" == "1" ]]; then
  SAFE_FLAG=""
fi

mkdir -p "$REPORT_DIR"

OPTS=()
if [[ -n "${VANTYX_ZAP_SESSION_COOKIE:-}" ]]; then
  OPTS+=(--hook=/zap/wrk/hooks.py)
fi

if [[ -z "${VANTYX_ZAP_SESSION_COOKIE:-}" ]] && [[ -n "${VANTYX_ZAP_PASSWORD:-}" ]]; then
  eval "$("$ROOT/scripts/zap/login.sh" "$LOGIN_TARGET")"
fi

DOCKER_NET=()
if [[ "${ZAP_USE_HOST_NETWORK:-1}" == "1" ]]; then
  DOCKER_NET=(--network host)
else
  DOCKER_NET=(--add-host=host.docker.internal:host-gateway)
fi

docker run --rm "${DOCKER_NET[@]}" \
  -e VANTYX_ZAP_SESSION_COOKIE \
  -v "$REPORT_DIR:/zap/wrk/:rw" \
  -v "$ROOT/docs/api/openapi.yaml:/zap/wrk/openapi.yaml:ro" \
  -v "$ROOT/scripts/zap/zap-rules.conf:/zap/wrk/zap-rules.conf:ro" \
  -v "$ROOT/scripts/zap/hooks.py:/zap/wrk/hooks.py:ro" \
  -t ghcr.io/zaproxy/zaproxy:stable \
  zap-api-scan.py \
  -t /zap/wrk/openapi.yaml \
  -f openapi \
  -O "$TARGET" \
  -c /zap/wrk/zap-rules.conf \
  "${OPTS[@]}" \
  $SAFE_FLAG \
  -r "${ZAP_API_REPORT_HTML:-zap-api-report.html}" \
  -J "${ZAP_API_REPORT_JSON:-zap-api-report.json}" \
  -w "${ZAP_API_REPORT_MD:-zap-api-report.md}" \
  -z "-config certificate.ignore=true" \
  -I
