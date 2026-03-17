#!/usr/bin/env bash
# Starts the Vantyx server for E2E tests. Run from repo root.
# Requires: vantyx-server binary and web/dist built.
# データは永続化しない（デフォルトで :memory:）。ファイルDBにする場合は VANTYX_E2E_DB を設定。
# Env: VANTYX_E2E_SERVER_BIN (default ./vantyx-server), VANTYX_E2E_DB (default :memory:)

set -e
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

BIN="${VANTYX_E2E_SERVER_BIN:-./vantyx-server}"
CERT_DIR="${VANTYX_E2E_CERT_DIR:-e2e/certs}"
mkdir -p "$CERT_DIR"

# E2E は永続化しない（:memory:）。ファイルで実行する場合は VANTYX_E2E_DB=e2e/test-data/e2e.db 等を指定
export VANTYX_SQLITE_PATH="${VANTYX_E2E_DB:-:memory:}"
if [[ "$VANTYX_SQLITE_PATH" != ":memory:" && "$VANTYX_SQLITE_PATH" != "" ]]; then
  mkdir -p "$(dirname "$VANTYX_SQLITE_PATH")"
fi
export VANTYX_EXTERNAL_HOST="localhost"
export VANTYX_HTTPS_ADDR=":18443"
export VANTYX_HTTP_REDIRECT_ADDR=":18080"
export VANTYX_TLS_CERT_FILE="$CERT_DIR/tls.crt"
export VANTYX_TLS_KEY_FILE="$CERT_DIR/tls.key"
# E2E は各テストでログインするためレート制限を緩和
export VANTYX_LOGIN_RATE_LIMIT_N=200

if [[ ! -f "$BIN" ]]; then
  echo "E2E server binary not found: $BIN (build with: go build -o vantyx-server ./cmd/vantyx-server)" >&2
  exit 1
fi

exec "$BIN"
