# セキュリティノート（ベストエフォート）

[English](SECURITY-ASVS-L2.md)

Vantyx は個人開発の小規模 OSS ですが、SSH / RDP / VNC / Telnet サーバへの
リモートアクセスを終端するため、機密データの保存・通信については
[OWASP ASVS](https://owasp.org/www-project-application-security-verification-standard/)
Level 2 の精神に倣うよう努めています。**これは正式な監査結果ではなく、
ベストエフォートのセルフチェックリスト**です。概要として読み、正確な
挙動はコードを参照してください。

## 実装している対策

| 領域 | 内容 | 主な場所 |
|------|------|---------|
| パスワード認証 | bcrypt ハッシュ、複雑性ポリシー（8 文字以上 + 大/小/数字/記号）、初期 admin パスワードの強制変更 | `internal/auth/password.go` |
| セッション | `crypto/rand` 由来の 32 バイトランダム ID、サーバ側ストア、24 時間 TTL、ログイン時に再発行、ログアウトで失効 | `internal/auth/session.go` |
| Cookie | `HttpOnly`、TLS 配下では `Secure`、`SameSite=Lax`、`MaxAge=24h` | `internal/httpapi/auth_handler.go` |
| レート制限 | IP あたり 15 分で 5 失敗 → `429`。既定で `RemoteAddr` を信頼、`X-Forwarded-For` は `VANTYX_TRUST_X_FORWARDED_FOR` でオプトイン | `internal/httpapi/login.go` |
| アクセス制御 | ユーザー単位のタグ / グループフィルタ、管理者専用ルートは `requireAdmin`、ターゲットスコープは `getSessionAndTargetWithAccess` | `internal/access`、`internal/httpapi/auth_helpers.go` |
| 入力バリデーション | ID / ホスト名は許可リストの正規表現 + 長さ制限、プロトコルは列挙、SQL は必ずプレースホルダ経由 | `internal/access/sqlite_store.go` |
| TLS | HTTPS 必須、TLS 1.3 以上、HTTP → HTTPS リダイレクト、HTTPS 応答に HSTS | `cmd/vantyx-server`、`internal/httpapi/middleware.go` |
| 保存時の暗号化 | 保存対象の SSH パスワードは AES-256-GCM で暗号化、鍵は `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY`、平文バッファは zeroise | `internal/secret` |
| エラー処理 | 5xx は `writeInternalError` / `writeServiceUnavailableError` を使い、内部情報を漏らさない（詳細はログへ） | `internal/httpapi/helpers.go` |
| CSRF / WS Origin | Cookie 認証の変更系には同一オリジン Origin / Referer チェック、WebSocket は `CheckOrigin` で同一オリジン（または `VANTYX_WS_ALLOWED_ORIGINS`）を強制 | `internal/httpapi/router.go`、`internal/httpapi/terminal.go` |
| セキュリティヘッダ | `Content-Security-Policy`、`X-Frame-Options: DENY`、`X-Content-Type-Options: nosniff`、`Cache-Control: no-store` | `internal/httpapi/middleware.go` |
| リクエスト上限 | 非 multipart はグローバルに `MaxBytesReader` で 2 MiB、ファイルアップロードは 64 MiB | `internal/httpapi/files.go` |
| 監査 | 認証イベント・ターミナル接続/拒否・ターゲット CRUD を `internal/logging` で記録、オプションで syslog / SIEM 転送 | `internal/logging`、`internal/httpapi/audit_forwarder.go` |

## 既知のギャップと運用上の注意

- **パスワード変更時にセッションを無効化しない** — 既存セッションは TTL
  が切れるまで有効です。ポリシー上必要ならアプリ側で追加実装してください。
- **暗号鍵ローテーション** — 保存済み SSH パスワードを再暗号化する仕組み
  は未実装です。`VANTYX_SSH_PASSWORD_ENCRYPTION_KEY` をローテーションすると
  既存レコードは復号不能になります。
- **Swagger UI 用 CSP** — `/docs` は Swagger UI のため `script-src` /
  `style-src` で `unpkg` を許可しています。アプリ本体側では許可していません。
- **自己署名 TLS** — 既定で自動生成される自己署名証明書は評価用です。
  本番では `VANTYX_TLS_CERT_FILE` / `VANTYX_TLS_KEY_FILE` で運用者の証明書を
  マウントしてください。
- **`VANTYX_DISABLE_ORIGIN_CHECK=1`** — 非ブラウザの自動化用に限定。
  公開デプロイでは絶対に有効化しないでください。
