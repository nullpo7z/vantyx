# OWASP ZAP 脆弱性診断

Vantyx 向けの ZAP ラッパースクリプトです。レポートはリポジトリ直下の `zap-reports/` に出力されます。

## 前提

- Docker が利用できること
- 診断対象の Vantyx が起動していること（**HTTPS `:8443` 推奨**）
- 未認証スキャン: 公開面のみ
- 認証スキャン: 管理者パスワードを環境変数で渡す

```bash
export VANTYX_ZAP_PASSWORD='your-admin-password'
# または初回起動時の VANTYX_INITIAL_ADMIN_PASSWORD
```

## スキャン実行

```bash
chmod +x scripts/zap/*.sh

# ベースライン（未認証）
ZAP_TARGET=https://host.docker.internal:8443 ./scripts/zap/run-baseline.sh

# ベースライン（ログイン後 /api/* 含む）
./scripts/zap/run-baseline-auth.sh

# OpenAPI（デフォルトはセーフ＝アクティブ攻撃なし）
./scripts/zap/run-api-scan.sh

# OpenAPI + アクティブ（ステージングのみ）
ZAP_API_ACTIVE=1 ./scripts/zap/run-api-scan.sh
```

## セキュリティヘッダ

アプリケーションは `internal/httpapi/security_headers.go` で全ルートに CSP・X-Frame-Options・Permissions-Policy 等を付与します。HTTPS では HSTS も付与されます（`r.TLS != nil`）。

## ルール調整

`scripts/zap/zap-rules.conf` で INFO / 誤検知を IGNORE にできます。

## 参考

- [ZAP Docker スキャン](https://www.zaproxy.org/docs/docker/)
- [SECURITY-ASVS-L2.md](../../docs/SECURITY-ASVS-L2.md)
