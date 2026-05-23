# OWASP ASVS Level 2 セキュリティチェック結果

本ドキュメントは Vantyx を OWASP Application Security Verification Standard (ASVS) Level 2 に照らして確認した結果です。

## 0. 重要ギャップと対応状況（優先度）

- **P0（重大）**
  - **WebSocket Origin 未検証（CSWSH）**: 対応済み（同一オリジン/allowlistに制限）
  - **RDP（ブラウザ）資格情報のURLクエリ**: 対応済み（クエリから受け取らない）
  - **localStorage に秘密情報一時保存**: 対応済み（BroadcastChannel に統一）
- **P1（高）**
  - **Cookie 認証の変更系に対する CSRF**: 対応済み（ブラウザ向け Origin/Referer 検証）
  - **共通のリクエストボディ上限**: 対応済み（multipart除外で 2MiB）
  - **X-Forwarded-For の信頼境界**: 対応済み（デフォルト不信、明示設定で利用）
  - **`target=_blank` の opener 悪用**: 対応済み（VNC/RDPは noopener、ターミナルは parent_token+noopener）
- **P2（中）**
  - **CSP 強化（/docs分離）**、**鍵ローテーション**、**パスワード変更時セッション失効**などは運用要件に応じて検討（下部参照）。

## 1. 認証 (Authentication)

| 要件 | 状態 | 備考 |
|------|------|------|
| V2.1 認証はパスワードまたは強力な代替手段を使用 | ✅ | bcrypt でパスワードをハッシュ保存 (`internal/auth/password.go`) |
| V2.2 一般的な認証情報・デフォルトパスワードの使用禁止 | ✅ | 初期パスワードは `Admin123!`（ポリシー準拠）。ログイン時に同一の場合は `require_password_change` を返し、変更を促す。 |
| V2.3 パスワード複雑さポリシー | ✅ | 8文字以上・大文字・小文字・数字・記号を必須（`auth.ValidatePassword`）。登録・変更時に検証。 |
| V2.4 ログアウトでセッション無効化 | ✅ | `POST /api/logout` でサーバー側セッション削除＋Cookie クリア |
| V2.5 アカウントロックアウト・ブルートフォース対策 | ✅ | 同一 IP で 15 分間に失敗 5 回を超えると 429 Too Many Requests。`LoginRateLimiter` で管理。 |
| V2.6 認証失敗時の汎用メッセージ | ✅ | 「invalid credentials」でユーザー列挙を抑制 |

## 2. セッション管理 (Session Management)

| 要件 | 状態 | 備考 |
|------|------|------|
| V3.1 セッション ID の乱数性 | ✅ | `crypto/rand` で 32 バイトを hex エンコード (`auth/session.go`) |
| V3.2 セッションの無効化（ログアウト・タイムアウト） | ✅ | ログアウトで Delete。Get 時に有効期限チェック（24h TTL） |
| V3.3 Cookie の HttpOnly / Secure / SameSite | ✅ | HttpOnly, SameSite=Lax, Secure（TLS 時）, MaxAge=24h |
| V3.4 セッション固定化対策 | ✅ | ログイン成功時に新規セッション ID を発行 |
| V3.x WebSocket のセッション保護 | ✅ | Cookie 認証 + **WebSocket Origin 検証**で CSWSH を緩和（`internal/httpapi/terminal.go`） |

## 3. アクセス制御 (Access Control)

| 要件 | 状態 | 備考 |
|------|------|------|
| V4.1 認証済みでないと機密 API にアクセス不可 | ✅ | 各ハンドラで Cookie 検証。未認証は 401 |
| V4.2 水平・垂直アクセス制御 | ✅ | ターゲット/グループは `TargetIDsForUser` でユーザーごとにフィルタ。他ユーザーの target_id は 403 |
| V4.3 管理機能のアクセス制限 | ✅ | `/api/spec`, `/docs` は `requireAdmin` で admin のみ |
| V4.4 認可はサーバー側で実施 | ✅ | ターゲット一覧・ターミナル接続はすべてサーバーで権限チェック |

## 4. 入力検証・エンコーディング

| 要件 | 状態 | 備考 |
|------|------|------|
| V5.1 入力のホワイトリスト・型・長さの検証 | ✅ | グループ/ターゲット ID は正規表現・長さ制限 (`access/sqlite_store.go`)。プロトコルは ssh/telnet のみ |
| V5.2 SQL インジェクション対策 | ✅ | プレースホルダ `?` 使用（ExecContext/QueryRowContext）。文字列連結なし |
| V5.3 出力エンコーディング（XSS） | ✅ | フロントで `escapeHtml()` によりユーザー由来の表示をエスケープ |
| V5.4 危険な文字・パス操作の制御 | ✅ | ホスト名は IP または hostname パターン。filepath.Clean でパストラバーサル対策 |

## 5. 暗号 (Cryptography)

| 要件 | 状態 | 備考 |
|------|------|------|
| V7.1 機密データの転送は TLS | ✅ | HTTPS 必須。TLS 1.3 最小。HTTP は HTTPS へリダイレクト |
| V7.2 強力なアルゴリズム | ✅ | bcrypt（パスワード）, AES-256-GCM（SSH パスワード保存）, TLS 1.3 |
| V7.3 秘密鍵・鍵材料の管理 | ✅ | 暗号鍵は環境変数 `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY`（base64 32 バイト）。平文ゼロ化のコメントあり |
| V7.4 ランダムの品質 | ✅ | `crypto/rand` 使用（セッション ID, nonce） |

## 6. エラー処理・ログ

| 要件 | 状態 | 備考 |
|------|------|------|
| V8.1 エラー時にスタックトレース等をクライアントに返さない | ✅ | 5xx は `writeInternalError` / `writeServiceUnavailableError` で汎用メッセージのみ。実詳細はサーバーログに記録 |
| V8.2 監査ログ・認証イベントの記録 | ✅ | ログイン成功/失敗、ターミナル接続/拒否を log.Printf で記録 |

## 7. データ保護

| 要件 | 状態 | 備考 |
|------|------|------|
| V9.1 機密データの保存時の保護 | ✅ | SSH パスワードは AES-256-GCM で暗号化（`internal/secret`）。鍵は環境変数 |
| V9.2 個人情報・機密データの最小化 | ✅ | セッションは user_id と有効期限のみ。パスワードは保存しない（ハッシュのみ） |

## 8. 通信 (Communication)

| 要件 | 状態 | 備考 |
|------|------|------|
| V10.1 TLS の使用 | ✅ | 本番想定は HTTPS。TLS 1.3 |
| V10.2 証明書の検証 | ⚠️ | クライアント側（ブラウザ）がサーバー証明書を検証。自己署名時はユーザーが例外追加が必要 |
| V10.3 安全なリダイレクト | ✅ | リダイレクト先は VANTYX_EXTERNAL_HOST / VANTYX_ALLOWED_HOSTS で制御 |

## 9. セキュリティヘッダ・設定

| 要件 | 状態 | 備考 |
|------|------|------|
| V14.4.1 X-Frame-Options | ✅ | DENY |
| V14.4.2 CORS | ✅ | VANTYX_CORS_ALLOWED_ORIGINS で明示的オリジンのみ許可 |
| V14.4.3 Content-Security-Policy | ✅ | default-src 'self'; script-src に wasm-unsafe-eval（録画プレイヤー用）; script/style は self + unpkg（Swagger UI）; frame-ancestors 'none' |
| V14.4.4 X-Content-Type-Options | ✅ | nosniff |
| V14.4.6 HSTS | ✅ | Strict-Transport-Security（HTTPS レスポンスのみ） |
| V14.4 Cache-Control | ✅ | no-store, max-age=0 |

## 10. API・Web サービス

| 要件 | 状態 | 備考 |
|------|------|------|
| 認証の一貫性 | ✅ | API は Cookie ベース認証。WebSocket /ws/ssh も Cookie 必須 |
| 認可の一貫性 | ✅ | ターゲット・グループ・ターミナルはすべてサーバーでユーザー権限チェック |
| マスアサインメント対策 | ✅ | 作成/更新は必要なフィールドのみ受け取り（createGroupRequest 等） |
| CSRF（Cookie 認証の変更系） | ✅ | 変更系リクエストは **同一オリジン Origin/Referer を要求**（主にブラウザ向け、`internal/httpapi/router.go`） |
| リクエストサイズ制限 | ✅ | 共通 `MaxBytesReader`（2MiB）＋アップロードは別途 64MiB（`internal/httpapi/router.go`, `internal/httpapi/files.go`） |

## 実施した改善（本チェックに伴う変更）

1. **Cookie の Secure / MaxAge**  
   ログイン時に `Secure`（TLS 時）と `MaxAge: 24*3600` を設定し、ブラウザとサーバー TTL を一致させた。

2. **サーバー側ログアウト**  
   `POST /api/logout` を追加。セッションをストアから削除し、Cookie を無効化。フロントはログアウト時にこの API を呼ぶ。

3. **ログインのレート制限（ASVS V2.5）**  
   同一 IP で 15 分間に失敗 5 回を超えると `429 Too Many Requests`。`App.LoginRateLimiter` で管理。

4. **パスワードポリシー（ASVS V2.3）**  
   `auth.ValidatePassword`: 8文字以上・大文字・小文字・数字・記号を必須。`CreateUser` および `UpdatePassword` で検証。初期パスワードを `Admin123!` に変更。

5. **初期パスワードの強制変更（ASVS V2.2）**  
   ログイン応答に `require_password_change: true` を返す（admin かつパスワードがデフォルトのとき）。フロントでパスワード変更画面を表示。`POST /api/me/password` で現在パスワード・新パスワードを送信し、`UserStore.UpdatePassword` で更新。

6. **V8.1 内部エラー漏洩の排除**  
   500/503 応答で `err.Error()` を返していた箇所を、`writeInternalError` / `writeServiceUnavailableError` に統一。クライアントには "internal error" / "service unavailable" のみ返し、詳細はサーバー側でログ出力。

7. **WebSocket Origin 検証（CSWSH 緩和）**  
   `websocket.Upgrader.CheckOrigin` を同一オリジン（または `VANTYX_WS_ALLOWED_ORIGINS`）に制限し、クロスサイトからの WebSocket 乗っ取りを緩和。`Origin` 欠落をデフォルト拒否（ローカル用途のみ `VANTYX_ALLOW_WS_NO_ORIGIN=1`）。

8. **RDP（ブラウザ）資格情報のURLクエリ排除**  
   `/ws/rdp/browser` で `rdp_user`/`rdp_pass` を URL クエリから受け取らない（履歴・ログ等への残留を回避）。保存済み資格情報のみ利用。

9. **ブラウザ永続ストレージ（localStorage）への秘密情報保存の撤廃**  
   ターミナル接続の不足情報（パスワード/パスフレーズ）を localStorage に保存する方式を廃止し、`BroadcastChannel` に統一。

10. **Cookie認証の変更系APIに対するCSRF緩和**  
   Cookie を持つ変更系（POST/PUT/PATCH/DELETE）は、同一オリジンの `Origin` または `Referer` を検証（主にブラウザ向け）。非ブラウザの自動化を想定する場合は `VANTYX_DISABLE_ORIGIN_CHECK=1` で無効化可能。

11. **共通のリクエストボディ上限（DoS 緩和）**  
   multipart 以外に `MaxBytesReader` を適用（2MiB）。アップロードは既存の 64MiB 制限を維持。

12. **`X-Forwarded-For` の信頼境界の明確化**  
   ログイン失敗レート制限のクライアントIPはデフォルトで `RemoteAddr` を使用し、明示的に `VANTYX_TRUST_X_FORWARDED_FOR=1` のときのみXFFを利用。

13. **`target=_blank` の `noopener` 化（タブナビング緩和）**  
   VNC/RDP の新タブ遷移に `rel=\"noopener noreferrer\"` を付与。ターミナルは `parent_token`（BroadcastChannel）で親タブ復帰できるため、`window.open(...,'noopener')` を利用。

## 推奨する追加対策（任意）

- **監査ログの永続化**: 現状は標準出力。本番ではファイル/外部ログ基盤への出力を検討。
- **パスワード変更時の全セッション失効**: パスワード変更後に他端末セッションを失効させる運用要件がある場合は実装を検討。
- **CSP の分離強化**: `/docs`（Swagger UI）とアプリ本体で CSP を分け、可能な範囲で `unsafe-inline` 等を削減。
- **暗号鍵ローテーション設計**: `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY` のローテーション/移行手順（複数キー対応、世代管理）を検討。
- **Host header 検証（HTTPS側）**: デプロイ形態によっては HTTPS 本体でも Host allowlist を検討。

---

*最終確認: 2026年。ASVS 4.0 Level 2 を基準に、コードベース検証に基づくセキュリティチェックを実施。*
