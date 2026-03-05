# Vantyx フロントエンド

- **開発**: バックエンドを別ターミナルで `go run ./cmd/vantyx-server`（:8080）、本ディレクトリで `npm run dev`（:5173）。Vite が /api と /ws を 8080 にプロキシします。
- **本番**: `npm run build` で `dist/` を生成し、Go サーバーが `web/dist` を自動で配信します（同一オリジンで API・WebSocket 利用可能）。

## 利用手順

1. ログイン: 初期ユーザー `admin` / `admin123!`
2. ターゲット一覧の「接続」→ ターゲットの SSH ユーザー名・パスワードを入力 → ターミナルが開きます。
