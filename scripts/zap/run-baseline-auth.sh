#!/usr/bin/env bash
# OWASP ZAP baseline scan with admin session cookie (authenticated surface).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
REPORT_DIR="${ZAP_REPORT_DIR:-$ROOT/zap-reports}"
TARGET="${ZAP_TARGET:-https://127.0.0.1}"
# Login from the host (cookie); ZAP may use the same target with --network host.
LOGIN_TARGET="${VANTYX_LOGIN_TARGET:-$TARGET}"

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
  -v "$REPORT_DIR:/zap/wrk/:rw" \
  -v "$ROOT/scripts/zap/zap-rules.conf:/zap/wrk/zap-rules.conf:ro" \
  -v "$ROOT/scripts/zap/hooks.py:/zap/wrk/hooks.py:ro" \
  -t ghcr.io/zaproxy/zaproxy:stable \
  zap-baseline.py \
  -t "$TARGET" \
  -c /zap/wrk/zap-rules.conf \
  --hook=/zap/wrk/hooks.py \
  -z "-config certificate.ignore=true" \
  -r zap-baseline-auth-report.html \
  -J zap-baseline-auth-report.json \
  -w zap-baseline-auth-report.md \
  -m 15 \
  -I
