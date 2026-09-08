# OWASP ZAP 脆弱性診断サマリ（Vantyx）

**最終更新:** 2026-06-12（セキュリティヘッダ適用後・認証付き ZAP 再スキャン）

## 対応内容

1. **セキュリティヘッダ** — `internal/httpapi/security_headers.go` を全ルート（`NewRouter`）に適用。CSP / X-Frame-Options / nosniff / Permissions-Policy / COOP / CORP / COEP / Referrer-Policy / no-store。HTTPS 時のみ HSTS。
2. **ZAP 再実行用スクリプト** — `scripts/zap/`（未認証・認証済みベースライン、OpenAPI、full scan、`zap-rules.conf`）。
3. **誤検知低減** — ホスト入力プレースホルダを RFC 5737 の `203.0.113.10` に変更（Private IP Disclosure 対策）。

## 推奨スキャン手順

```bash
export VANTYX_ZAP_PASSWORD='…'
export ZAP_TARGET=https://127.0.0.1   # または https://host.docker.internal:8443
eval "$(./scripts/zap/login.sh)"
export VANTYX_ZAP_SESSION_COOKIE
./scripts/zap/run-baseline-auth.sh
ZAP_API_ACTIVE=1 ./scripts/zap/run-api-scan.sh
./scripts/zap/run-full-scan-auth.sh
```

`:8080` はリダイレクト専用のため、**HTTPS（`:443` / `:8443`）** を対象にしてください。

## 最新診断結果（2026-06-12・HTTPS・認証済み・`:dev` イメージ）

| スキャン | FAIL | WARN | PASS |
|---------|------|------|------|
| OpenAPI アクティブ | 0 | 1（Content-Type 誤検知） | 119 |
| Full（Spider + Active） | 0 | 5（CSP ws/wss、COEP unsafe-none、JS 誤検知等） | 137 |

Permissions-Policy / CORP / COOP は PASS。残 WARN は REST/SPA/WebSocket 向けの許容設定またはスキャナ誤検知。

## 以前の診断結果（HTTP :8080・ヘッダ未適用時）

| 深刻度 | 件数 |
|--------|------|
| High / Fail | 0 |
| Medium (Warn) | CSP・クリックジャッキング対策 |
| Low | nosniff、Permissions-Policy、CORP 等 |

ヘッダ適用後は上記 Warn の多くが解消される想定です。再スキャンで確認してください。

## 限界

- WebSocket・SSH・VNC・RDP プロトコル本体は HTTP スキャンの対象外
- 認証済み API のアクティブ攻撃は `run-api-scan.sh` で `ZAP_API_ACTIVE=1`（ステージングのみ）
- 業務ロジック・IDOR は手動レビューが必要

詳細: [scripts/zap/README.ja.md](../scripts/zap/README.ja.md)
