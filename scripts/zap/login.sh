#!/usr/bin/env bash
# Obtain vantyx_session cookie for authenticated ZAP scans.
# Usage: eval "$(./scripts/zap/login.sh)"   # uses ZAP_TARGET or https://localhost:8443
#        eval "$(./scripts/zap/login.sh https://127.0.0.1)"
set -euo pipefail

TARGET="${1:-${ZAP_TARGET:-${VANTYX_LOGIN_TARGET:-https://localhost:8443}}}"
USER="${VANTYX_ZAP_USER:-zap}"
PASS="${VANTYX_ZAP_PASSWORD:-${VANTYX_INITIAL_ADMIN_PASSWORD:-}}"

if [[ -z "$PASS" ]]; then
  echo "Set VANTYX_ZAP_PASSWORD or VANTYX_INITIAL_ADMIN_PASSWORD" >&2
  exit 1
fi

COOKIE_JAR="$(mktemp)"
trap 'rm -f "$COOKIE_JAR"' EXIT

curl -sk -c "$COOKIE_JAR" -X POST "${TARGET%/}/api/login" \
  -H 'Content-Type: application/json' \
  -H 'Origin: '"${TARGET%/}" \
  -d "{\"username\":\"${USER}\",\"password\":\"${PASS}\"}" \
  -o /dev/null -w '%{http_code}' | grep -q '^200$' || {
  echo "login failed (check password / forced password change)" >&2
  exit 1
}

VAL="$(awk '$6=="vantyx_session"{print $7}' "$COOKIE_JAR" | tail -1)"
if [[ -z "$VAL" ]]; then
  echo "vantyx_session cookie not found" >&2
  exit 1
fi

echo "export VANTYX_ZAP_SESSION_COOKIE='vantyx_session=${VAL}'"
