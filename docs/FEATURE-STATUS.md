# Vantyx 機能一覧（実装済み・未実装）

計画（`docs/plan/vantyx-implementation_f61a7d8e.plan.md`）およびコードベースに基づく一覧です。

---

## 実装済み機能

### 基盤・プロジェクト構成
- Go バックエンド + Tailwind ベースのフロントエンド（SPA）の最小構成
- 環境変数・`.env` による設定読込
- SQLite 永続化（マイグレーション含む）
- Docker / docker-compose による 1 コンテナ起動（backend + frontend 同一コンテナ）
- ヘルスチェック `/healthz`
- HTTPS 必須・HTTP→HTTPS リダイレクト・自己署名証明書の自動生成

### 認証・認可（Phase 1）
- ローカルユーザー管理（作成・一覧・パスワード変更）
- パスワード認証（bcrypt ハッシュ、複雑さポリシー 8 文字以上・大小数字・記号）
- セッション管理（Cookie + サーバー側セッション、24h TTL）
- ログイン / ログアウト / 初期パスワード変更の強制
- ログイン失敗のレート制限（同一 IP で 15 分間に 5 回超で 429）
- ロール（admin / 一般）と管理機能のアクセス制限（`/api/spec`, `/docs` は admin のみ）
- アクセスグループ・ユーザー・ターゲットの多対多（グループ↔ターゲット、ユーザー↔グループ、タグによる紐付け）

### SSH/Telnet・セッション永続化・CLI アクセス（Phase 2）
- ブラウザターミナル（xterm.js + WebSocket `/ws/ssh`）— **SSH と Telnet** ターゲットに対応
- SSH プロキシ（`internal/sshproxy`）、Telnet プロキシ（`internal/telnetproxy`）、セッション管理（`internal/session`）
- Telnet: NAWS（端末リサイズ）、保存認証による自動ログイン
- SSH/Telnet ターミナル接続失敗時の英語診断メッセージ（TCP dial タイムアウト・拒否等）
- セッション永続化・レジューム（再接続で同一セッションにアタッチ）。Web タブを閉じた場合はバックグラウンド継続（デタッチ）し、一覧の「終了」または `DELETE /api/terminal/sessions/{id}` で明示終了
- CLI セッション操作: Ctrl+] でメニューへ detach（Web タブ閉じ相当・セッション継続）、`connect` 中の Ctrl+D でセッション終了
- **継続中セッション一覧**（ナビ「セッション」）: ログインユーザーがアクセス権を持つターゲット上の自分のセッションのみ表示（サーバー側フィルタ）。SSH/Telnet/RDP の再接続・終了、SSE による一覧更新
- **無活動（idle）警告**: `VANTYX_TERMINAL_SESSION_IDLE_WARN_AFTER`（デフォルト 30 分、`0` で無効）。ターミナルはクライアント操作とリモート出力、RDP（ブラウザ）は VNC 入出力（操作・画面更新）で `last_seen` を更新。デタッチ後もバックグラウンドの VNC 監視で更新。API の `last_seen` / `idle`、Web 一覧・ホームバナー・CLI 一覧での表示。**自動終了はしない**
- ターミナルセッション一覧・削除 API（ターゲット権限チェック付き）
- **CLI アクセス**: Vantyx が SSH サーバーとして待ち受け（`internal/sshd`）、Tera Term 等から `ssh user@vantyx` でログインし、ターゲットへプロキシ接続
- SSH 公開鍵登録（`/api/me/ssh-keys`）による Vantyx SSH ログイン
- ターゲットプロトコル: **SSH** / **Telnet** / **VNC**（作成・編集・一覧で選択可能）

### 録画・証跡ログ（Phase 2 / Phase 3 の一部）
- Asciinema 形式でのターミナル録画（`internal/recording`）
- 録画メタデータの DB 保存（`VANTYX_RECORDINGS_DIR` 設定時）
- 録画一覧 API（`GET /api/recordings`）・録画ファイル取得（`GET /api/recordings/{id}/file`）
- フロント: 録画一覧表示・Asciinema プレイヤーでの再生

### ファイル転送（Phase 4 の一部）
- **SFTP** バックエンド（`internal/sftp`）: ターゲットへのリスト・アップロード・ダウンロード・削除
- ファイル操作 API: `GET/POST/GET/DELETE /api/targets/{id}/files/*`
- ファイルマネージャー UI（`/files`）: ディレクトリツリー・一覧・アップロード・ダウンロード・削除

### VNC リモートデスクトップ（Phase 5 の一部）
- **VNC** WebSocket プロキシ（`internal/vncproxy`）: ブラウザ↔VNC サーバー間のバイトブリッジ
- `GET /ws/vnc?target_id=...` で noVNC クライアントと接続
- フロント: `/vnc` ページで noVNC（CDN）を利用した VNC ビューア

### ターゲット・グループ・タグ
- ターゲット CRUD、グループへの紐付け、パス・SSH 認証情報（パスワード・秘密鍵）の保存
- SSH パスワードの保存時暗号化（AES-256-GCM、`VANTYX_SSH_PASSWORD_ENCRYPTION_KEY`）
- グループ・ユーザー・ターゲットへのタグ付与と、タグ一致によるアクセス制御
- グループメンバー管理（追加・削除）

### セキュリティ・ASVS L2 対応
- OWASP ASVS Level 2 に沿った設計（`docs/SECURITY-ASVS-L2.md` 参照）
- Cookie の HttpOnly / Secure / SameSite、セッション固定化対策、CSRF 考慮
- 入力検証（ID・ホスト名・プロトコルのホワイトリスト）、SQL プレースホルダ、XSS 対策（escapeHtml）
- 監査ログ（ログイン・ターミナル接続等を log 出力）

### API・フロントエンド
- REST API: ログイン/ログアウト/me/パスワード変更、グループ/ユーザー/ターゲット/タグ/録画/ファイル操作
- Swagger 風 API 仕様（`/api/spec`）・管理用ドキュメント（`/docs`）
- SPA: ログイン、ダッシュボード、グループツリー、ターゲット一覧・編集、継続中セッション一覧、ターミナル/VNC/ファイル/録画への導線

---

## 未実装機能

### ファイル転送（Phase 4）
- **TFTP**: プロトコル・バックエンド・UI のいずれも未実装（SFTP のみ対応）

### リモートデスクトップ（Phase 5）
- **RDP ブリッジ**: FreeRDP ラップまたは Go 実装の RDP プロキシ未実装（VNC のみ対応）

### SmartJumper 互換・外部認証（Phase 3）
- **外部認証**: RADIUS / TACACS+ / LDAP 連携は未実装
- **SSL 証明書インポート**: 管理 UI からの証明書インポートは未実装（証明書はファイル配置で対応）

### 証跡・ログ（Phase 3 の一部）
- **証跡ログの高度な検索 UI**: 録画一覧・ダウンロードはあるが、期間・ユーザー・ターゲット等での絞り込み検索 UI は未整備（API はクエリパラメータで対応している可能性あり要確認）
- **監査ログの永続化**: 現状は標準出力のみ（ファイル/外部ログ基盤への出力は未実装）

### 認証・MFA（Phase 6）
- **OIDC SSO**: 外部 IdP（Keycloak / Azure AD 等）との OIDC 連携は未実装
- **TOTP MFA**: TOTP による多要素認証は未実装

### インフラ・運用
- **PostgreSQL**: 現状は SQLite のみ（計画上の「RDB（PostgreSQL 想定）」は未対応）
- **オブジェクトストレージ**: 録画・ファイルの外部ストレージ連携は未実装（ローカル FS のみ）
- **K8s 用マニフェスト**: `deploy/k8s` 等の Kubernetes 用配置は未実装

### その他
- **構造化ログ**: zap/logrus 等の構造化ログは未導入（`log.Printf` ベース）
- （Telnet ブラウザ/CLI ターミナルは実装済み — 下記「実装済み」参照）

---

## まとめ

| カテゴリ           | 実装済み                           | 未実装                                   |
|--------------------|------------------------------------|------------------------------------------|
| 認証・セッション   | ローカル認証、Cookie セッション、レート制限 | OIDC、TOTP、RADIUS/TACACS+/LDAP          |
| アクセス制御       | グループ・ターゲット・タグ、水平/垂直制御   | （特になし）                             |
| ターミナル         | SSH/Telnet プロキシ、ブラウザ+CLI、永続化・レジューム、Telnet NAWS/自動ログイン | —                                        |
| 録画・証跡         | Asciinema 録画、一覧・再生・ダウンロード   | 高度な検索 UI、監査ログ永続化            |
| ファイル転送       | SFTP、ファイルマネージャー UI              | TFTP                                     |
| リモートデスクトップ | VNC（noVNC + WebSocket プロキシ）          | RDP ブリッジ                             |
| セキュリティ       | ASVS L2 対応、暗号化、ヘッダ・CORS 等      | 監査ログ永続化                           |
| インフラ           | Docker 1 コンテナ、SQLite、自己署名 TLS    | PostgreSQL、オブジェクトストレージ、K8s   |

*最終更新: 計画書・コードベースに基づく。実装の詳細は各パッケージ（`internal/*`）を参照。*
