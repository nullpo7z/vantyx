#!/usr/bin/env bash
# OWASP ZAP baseline scan (unauthenticated). Prefer HTTPS in production.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
REPORT_DIR="${ZAP_REPORT_DIR:-$ROOT/zap-reports}"
TARGET="${ZAP_TARGET:-https://127.0.0.1}"

mkdir -p "$REPORT_DIR"

DOCKER_NET=()
if [[ "${ZAP_USE_HOST_NETWORK:-1}" == "1" ]]; then
  DOCKER_NET=(--network host)
else
  DOCKER_NET=(--add-host=host.docker.internal:host-gateway)
fi

# shellcheck disable=SC2086
docker run --rm "${DOCKER_NET[@]}" \
  -v "$REPORT_DIR:/zap/wrk/:rw" \
  -v "$ROOT/scripts/zap/zap-rules.conf:/zap/wrk/zap-rules.conf:ro" \
  -t ghcr.io/zaproxy/zaproxy:stable \
  zap-baseline.py \
  -t "$TARGET" \
  -c /zap/wrk/zap-rules.conf \
  -r zap-baseline-report.html \
  -J zap-baseline-report.json \
  -w zap-baseline-report.md \
  -m 10 \
  -z "-config certificate.ignore=true" \
  -I \
  ${ZAP_EXTRA_OPTS:-}
