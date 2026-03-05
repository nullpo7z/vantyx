# Vantyx

アクセス管理ゲートウェイ（SSH ターミナル・認証・ターゲット管理）。1 コンテナの Docker で完結し、リバースプロキシは不要です。

## Docker で起動

```bash
docker compose up --build
```

- **HTTP (80)**: 常に **HTTPS へ 301 リダイレクト**（無効化不可）
- **HTTPS (443)**: アプリ本体（API + SPA）。コンテナ内では 8080/8443 で待ち受け、ホストの 80/443 にマッピングしています。
- **TLS**: 初回起動時に `/app/certs` に証明書が無い場合は **自己署名証明書** を自動生成します。ボリューム `vantyx_certs` で永続化されるため、2 回目以降は同じ証明書を使います。本番では `./certs` をマウントして自身の証明書を配置しても構いません（`VANTYX_TLS_CERT_FILE`, `VANTYX_TLS_KEY_FILE` でパス変更可）。
- ブラウザでは **https://localhost** でアクセスし、自己署名の場合は警告を許可してください。
- 初期ユーザー: `admin` / `admin123!`

## 開発（ローカル）

- バックエンド: `VANTYX_TLS_CERT_FILE=./certs/tls.crt VANTYX_TLS_KEY_FILE=./certs/tls.key go run ./cmd/vantyx-server`（初回は `./certs` に自己署名を自動生成。HTTP :8080 → HTTPS へリダイレクト、HTTPS :8443）
- フロントエンド: `cd web && npm run dev`（:5173、/api と /ws はバックエンドにプロキシ）

詳細は [web/README.md](web/README.md) を参照してください。
