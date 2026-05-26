#!/usr/bin/env bash
# E2E を Docker 上のサーバーに対して実行する。
# 1) E2E 用証明書を用意（無ければ自己署名を生成）
# 2) docker compose -f docker-compose.e2e.yml up -d
# 3) サーバーが応答するまで待機
# 4) e2e ディレクトリで Playwright を実行
# 5) docker compose down
# リポジトリルートで実行すること。

set -e
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

CERT_DIR="${REPO_ROOT}/e2e/certs"
COMPOSE_FILE="${REPO_ROOT}/docker-compose.e2e.yml"

mkdir -p "$CERT_DIR" "${REPO_ROOT}/e2e/test-data"

# E2E 用の自己署名証明書が無ければ生成する
if [[ ! -f "$CERT_DIR/tls.crt" || ! -f "$CERT_DIR/tls.key" ]]; then
  echo "Generating E2E TLS cert in $CERT_DIR ..."
  openssl req -x509 -newkey rsa:2048 -keyout "$CERT_DIR/tls.key" -out "$CERT_DIR/tls.crt" \
    -days 365 -nodes -subj "/CN=localhost" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
fi

# 既存の E2E 用コンテナを止めておく
docker compose -f "$COMPOSE_FILE" down 2>/dev/null || true

# 毎回フレッシュな DB で実行するため既存の e2e.db を削除
# （以前 sudo で実行した場合 root 所有になっていることがある）
E2E_DB="${REPO_ROOT}/e2e/test-data/e2e.db"
if [[ -f "$E2E_DB" ]]; then
  rm -f "$E2E_DB" 2>/dev/null || {
    echo "Warning: could not remove $E2E_DB (permission denied). Trying removal via a short-lived container..." >&2
    # 以前 sudo で作成された DB は root 所有になりがちなので、ホスト側で削除できない場合は
    # volume を同じくマウントした一時コンテナ（root）で削除する。
    docker compose -f "$COMPOSE_FILE" build --quiet 2>/dev/null || docker compose -f "$COMPOSE_FILE" build
    docker compose -f "$COMPOSE_FILE" run --rm -T --user 0 --entrypoint sh vantyx -lc "rm -f /app/data/e2e.db || true" >/dev/null 2>&1 || {
      echo "Warning: could not remove $E2E_DB even via container. Tests will run with existing DB." >&2
    }
  }
fi

# E2E ごとにフレッシュな AES-256 鍵を生成して compose に渡す（リポジトリに固定鍵を残さない: CWE-321 / 798）。
if [[ -z "${VANTYX_SSH_PASSWORD_ENCRYPTION_KEY:-}" ]]; then
  export VANTYX_SSH_PASSWORD_ENCRYPTION_KEY="$(openssl rand -base64 32)"
fi

echo "Building and starting E2E server with Docker Compose ..."
docker compose -f "$COMPOSE_FILE" build --quiet 2>/dev/null || docker compose -f "$COMPOSE_FILE" build
docker compose -f "$COMPOSE_FILE" up -d

# サーバーが応答するまで待つ。デフォルトは 127.0.0.1（IPv4 固定で Cookie が安定しやすい）。失敗時は E2E_BASE_URL=https://localhost:18443 を試す。
E2E_BASE="${E2E_BASE_URL:-https://127.0.0.1:18443}"
echo "Waiting for $E2E_BASE to be ready ..."
for i in {1..60}; do
  if curl -k -s -o /dev/null -w "%{http_code}" "$E2E_BASE/" 2>/dev/null | grep -qE '200|301|302'; then
    echo "Server is up."
    break
  fi
  if [[ $i -eq 60 ]]; then
    echo "Timeout waiting for server. Logs:" >&2
    docker compose -f "$COMPOSE_FILE" logs --tail=30
    docker compose -f "$COMPOSE_FILE" down
    exit 1
  fi
  sleep 1
done

# Playwright 実行（e2e で npm run test）
# sudo で実行した場合は元のユーザーで npm を実行（root の PATH に node/npm がないため）
EXIT=0
run_playwright() {
  cd "$REPO_ROOT/e2e"
  if [[ ! -d node_modules ]]; then
    npm ci
    npx playwright install chromium
  fi
  E2E_BASE_URL="$E2E_BASE" E2E_VIDEO="${E2E_VIDEO:-}" npm run test
}
if [[ -n "$SUDO_USER" ]]; then
  su "$SUDO_USER" -c "export REPO_ROOT='$REPO_ROOT' E2E_BASE='$E2E_BASE' E2E_VIDEO='${E2E_VIDEO:-}'; cd \"\$REPO_ROOT/e2e\" && ( [[ ! -d node_modules ]] && npm ci && npx playwright install chromium; true ) && E2E_BASE_URL=\"\$E2E_BASE\" E2E_VIDEO=\"\$E2E_VIDEO\" npm run test" || EXIT=$?
else
  run_playwright || EXIT=$?
fi

# 失敗時に原因を追えるよう、down の前にコンテナ状態とログを保存する
if [[ $EXIT -ne 0 ]]; then
  echo ""
  echo "E2E failed (exit=$EXIT). Capturing docker logs before shutdown ..." >&2
  mkdir -p "$REPO_ROOT/test-results"
  docker compose -f "$COMPOSE_FILE" ps >&2 || true
  docker compose -f "$COMPOSE_FILE" logs --no-color >"$REPO_ROOT/test-results/e2e-docker-compose.log" 2>&1 || true
  echo "Saved docker compose logs to $REPO_ROOT/test-results/e2e-docker-compose.log" >&2
fi

# コンテナを止める
cd "$REPO_ROOT"
docker compose -f "$COMPOSE_FILE" down

exit $EXIT
