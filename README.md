# Vantyx

アクセス管理ゲートウェイ（SSH ターミナル・認証・ターゲット管理）。1 コンテナの Docker で完結し、リバースプロキシは不要です。

## Docker で起動

```bash
docker compose up --build
```

- **HTTP (80)**: 常に **HTTPS へ 301 リダイレクト**（無効化不可）
- **HTTPS (443)**: アプリ本体（API + SPA）。コンテナ内では 8080/8443 で待ち受け、ホストの 80/443 にマッピングしています。
- **TLS**: 初回起動時に `/app/certs` に証明書が無い場合は **自己署名証明書** を自動生成します。ボリューム `vantyx_certs` で永続化されるため、2 回目以降は同じ証明書を使います。本番では `./certs` をマウントして自身の証明書を配置しても構いません（`VANTYX_TLS_CERT_FILE`, `VANTYX_TLS_KEY_FILE` でパス変更可）。
- ブラウザでは **https://localhost** または **https://<サーバーIP>** でアクセスできます。自己署名の場合は警告を許可してください。
- **サーバーIPでの TLS**: 自己署名証明書に IP アドレスを含めるには `VANTYX_TLS_SANS` を指定します（例: `VANTYX_TLS_SANS=192.168.1.10`）。証明書は初回生成後に固定されるため、設定を変えた場合は `vantyx_certs` ボリュームを削除して再生成してください。
- 初期ユーザー: `admin` / `Admin123!`（本番では必ず変更すること）
- **データ永続化**: SQLite は環境変数 `VANTYX_SQLITE_PATH` でファイルパスを指定できます。未設定時は `data/vantyx.db` を使い、起動ディレクトリに `data/` を作成して永続化します。Docker ではボリュームでこのパスをマウントするとデータが残ります。

## セキュリティ（OWASP ASVS L2）

本アプリは機密データの保存において [OWASP ASVS](https://owasp.org/www-project-application-security-verification-standard/) Level 2 に準拠するよう設計しています。

- **保存時暗号化 (V7.12)**: サーバー登録時に保存する SSH パスワードは、AES-256-GCM で暗号化してから DB に格納します。
- **鍵の配置 (V7.14)**: 暗号化鍵はコードに含めず、インストール時に環境変数で注入します。**サーバー登録で SSH パスワードを保存する場合は必須**です。
- **鍵のゼロ化 (V7.13)**: 平文パスワードおよび復号後のバッファは利用後にゼロクリアします。

### SSH パスワード暗号化鍵の設定

サーバー登録フォームで「SSH パスワード」を保存する場合、環境変数 **`VANTYX_SSH_PASSWORD_ENCRYPTION_KEY`** に 32 バイトの鍵を Base64 で設定してください。未設定のままパスワードを保存しようとすると `503 Service Unavailable` になります。

**鍵の生成例（32 バイトを Base64）:**

```bash
openssl rand -base64 32
```

Docker Compose で渡す例: `docker compose run -e VANTYX_SSH_PASSWORD_ENCRYPTION_KEY="$(openssl rand -base64 32)" ...` または `env` ファイルに記載し `env_file` で読み込ませてください。**鍵をローテーションすると、既存の暗号化パスワードは復号できなくなります。** 鍵は安全に保管し、本番ではシークレット管理（Kubernetes Secret 等）の利用を推奨します。

## 開発（ローカル）

- バックエンド: `VANTYX_TLS_CERT_FILE=./certs/tls.crt VANTYX_TLS_KEY_FILE=./certs/tls.key go run ./cmd/vantyx-server`（初回は `./certs` に自己署名を自動生成。HTTP :8080 → HTTPS へリダイレクト、HTTPS :8443）。DB は未設定時 `data/vantyx.db` に永続化されます。
- フロントエンド: `cd web && npm run dev`（:5173、/api と /ws はバックエンドにプロキシ）

詳細は [web/README.md](web/README.md) を参照してください。
