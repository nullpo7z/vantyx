# E2E テスト（Web 操作のみ）

SSH / TELNET / VNC / RDP / SFTP / TFTP / FTP / SCP の接続テストは含みません。これらは手動で実行してください。

## 実行方法

### 方法 A: Docker でサーバーを起動してテスト（推奨）

Docker および Docker Compose が利用できる環境で、リポジトリルートで以下を実行します。Docker でサーバーを起動し、テスト実行後に自動でコンテナを停止します。

```bash
./scripts/run-e2e-with-docker.sh
```

- 初回は `docker compose -f docker-compose.e2e.yml build` と `e2e/certs` の自己署名証明書生成が走ります。
- サーバーは `https://localhost:18443` で待ち受け、毎回フレッシュな DB（`e2e/test-data/e2e.db`）で起動します。

### 方法 B: 手動でサーバーを起動してテスト

1. リポジトリルートでサーバーと Web をビルドする。

   ```bash
   go build -o vantyx-server ./cmd/vantyx-server
   cd web && npm ci && npm run build && cd ..
   ```

2. E2E 用サーバーを起動する（別ターミナルで）。

   ```bash
   ./scripts/run-e2e-server.sh
   ```

3. このディレクトリで Playwright をインストールし、テストを実行する。

   ```bash
   cd e2e
   npm ci
   npx playwright install chromium
   E2E_BASE_URL=https://localhost:18443 npm run test
   ```
## テスト項目一覧（56 件・23 ファイル）

| ファイル | 内容 |
|----------|------|
| `auth.spec.ts` | ログイン画面・不正認証・正しい認証・ログアウト |
| `account.spec.ts` | ユーザー名クリックでアカウント画面表示 |
| `change_password_modal.spec.ts` | パスワード変更モーダル（確認不一致エラー） |
| `navigation.spec.ts` | ホーム・録画・サーバー管理・ユーザー管理・API リファレンス |
| `groups.spec.ts` | グループ追加 |
| `group_tags_members.spec.ts` | グループのタグ編集、メンバー追加/削除、「追加できるユーザーがいません」 |
| `tags_behavior.spec.ts` | タグ挙動（複数タグ・空保存・トリム・ハイフン/アンダースコア・往復表示・サーバー/ユーザータグ） |
| `tag_access_control.spec.ts` | タグでの権限管理（一般ユーザーはタグ一致のグループ・ターゲットのみ表示） |
| `nested_group.spec.ts` | 親グループ選択して子グループ追加 |
| `targets.spec.ts` | グループ＋サーバー追加 |
| `target_edit_delete.spec.ts` | サーバー編集（名前/タグ）、サーバー削除 |
| `target_protocol_auth.spec.ts` | 編集フォームでプロトコル切替・デフォルトポート、SSH 認証方式表示切替 |
| `add_target_protocol_port.spec.ts` | 追加フォームでプロトコル切替でデフォルトポート変更 |
| `target_validation.spec.ts` | サーバー追加で名前空のときエラー |
| `users.spec.ts` | ユーザー一覧・ユーザー追加・管理者ロールで追加 |
| `user_edit_sshkeys.spec.ts` | ユーザーのタグ編集、SSH 公開鍵の追加/削除 |
| `role_non_admin.spec.ts` | 一般ユーザーでログインするとユーザー管理が非表示 |
| `validation_errors.spec.ts` | タグ不正・65文字超・ユーザーID不正のバリデーション |
| `modals_cancel.spec.ts` | 各モーダルを開いてキャンセル/×で閉じる（グループ・ユーザー・サーバー・編集・タグ・メンバー・パスワード変更） |
| `active_sessions_modal.spec.ts` | アクティブなセッションモーダルを開いて空メッセージ・閉じる |
| `file_protocol_modal.spec.ts` | ファイルボタンでファイル転送プロトコル選択モーダルを開いて閉じる |
| `recordings.spec.ts` | 録画ページ表示 |
| `recordings_ui.spec.ts` | 録画のグループ/サーバー選択、一覧、「← サーバー一覧」で戻る |

## CI について

このリポジトリでは **E2E を CI では実行しません**。ローカルでのみ実行してください。
