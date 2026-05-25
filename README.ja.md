# Vantyx

[English](README.md)

[![CI](https://github.com/nullpo7z/vantyx/actions/workflows/ci-dev.yml/badge.svg)](https://github.com/nullpo7z/vantyx/actions/workflows/ci-dev.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/nullpo7z/vantyx.svg)](https://pkg.go.dev/github.com/nullpo7z/vantyx)

Vantyx は、ブラウザや CLI クライアントと SSH / Telnet / RDP / VNC / SFTP /
FTP / TFTP サーバーをつなぐセルフホスト型のアクセスゲートウェイです。
1 コンテナで完結し、自身で TLS を終端、状態は SQLite に永続化、同梱の
シングルページ UI 向けに統一的な REST + WebSocket API を提供します。

## 特長

- **ブラウザターミナル**（SSH / Telnet 対応、xterm.js + WebSocket）：
  セッションの再開、NAWS、永続シェル。
- **ブラウザリモートデスクトップ**（noVNC 経由の VNC）：RDP ブリッジは
  ロードマップに掲載。
- **ファイル転送 UI**：SFTP / FTP / リモート TFTP / ネットワーク機器向け
  組み込み TFTP サーバー。
- **CLI ゲートウェイ**：`ssh user@vantyx` で Vantyx にログインし、許可
  されたターゲットへプロキシ接続。
- **セッション録画**：対話シェルを asciinema 形式で記録し、UI で再生。
- **監査パイプライン**：全ての API 呼び出し・セッションイベントを取得し、
  外部 syslog / SIEM 転送に対応。
- **OWASP ASVS Level 2** に準拠した機密データ保存・通信のベースライン。

## クイックスタート（Docker）

```bash
docker compose up --build
```

| ポート   | 用途                                                                                              |
|----------|---------------------------------------------------------------------------------------------------|
| `80`     | 常に HTTPS へ 301 リダイレクト（無効化不可）                                                       |
| `443`    | アプリ本体（REST API + SPA）。コンテナ内では `8080` / `8443` で待ち受け。                          |
| `69/udp` | コンテナ内 `6969/udp` の組み込み TFTP サーバーへ転送。                                             |

- **TLS**: 初回起動時、`/app/certs/tls.crt` と `tls.key` がなければ自己署名
  証明書を生成し `vantyx_certs` ボリュームに永続化します。証明書を差し替える
  には `VANTYX_TLS_CERT_FILE` / `VANTYX_TLS_KEY_FILE` でパスを指定してください。
- **初期管理者**: `admin` / `Admin123!` — 初回ログイン直後に変更してください。
- **データ永続化**: SQLite は既定で `data/vantyx.db`。`VANTYX_SQLITE_PATH`
  で変更し、コンテナでは当該ディレクトリをマウントしてください。

サーバー IP を証明書に含めるには `VANTYX_TLS_SANS`（カンマ区切りで DNS 名や IP）
を指定します。値を変更した場合は `vantyx_certs` ボリュームを削除して再生成
してください。

## アーキテクチャ

```mermaid
graph LR
    Browser["Web SPA<br/>(web/src)"] -->|HTTPS / WSS| HTTPAPI["HTTP API<br/>internal/httpapi"]
    CLI["CLI client<br/>(ssh user@vantyx)"] -->|SSH| SSHD["CLI gateway<br/>internal/sshd"]
    HTTPAPI --> Auth["Auth / Access<br/>internal/auth, internal/access"]
    HTTPAPI --> Bridges["Protocol bridges<br/>internal/sshproxy, telnetproxy,<br/>vncproxy, rdpvnc, sftp, ftp, tftp"]
    HTTPAPI --> Sessions["Session managers<br/>internal/session, internal/rdpvnc"]
    HTTPAPI --> DB[("SQLite<br/>internal/db/sqlite")]
    SSHD --> Bridges
    SSHD --> Sessions
    SSHD --> DB
    Bridges --> Targets[(("Remote SSH /<br/>Telnet / VNC /<br/>SFTP / FTP / TFTP"))]
```

詳細は [docs/ARCHITECTURE.ja.md](docs/ARCHITECTURE.ja.md) を参照してください。
公開 REST + WebSocket API は [docs/api/openapi.yaml](docs/api/openapi.yaml)
にドキュメント化され、管理者は `/docs`（Swagger UI）から参照できます。

## 設定

全ての実行時設定は `VANTYX_*` 名前空間にあります。完全な一覧は
[docs/configuration.md](docs/configuration.md) を参照してください。

本番環境で最低限必要な設定：

- `VANTYX_EXTERNAL_HOST` または `VANTYX_ALLOWED_HOSTS` — 安全な
  HTTP→HTTPS リダイレクトに必須。
- `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY` — 運用者がターゲットレコードに
  SSH パスワードを保存する場合に必須（AES-256-GCM、OWASP ASVS V7.12）。

```bash
# 32 バイト・Base64 の鍵を生成
openssl rand -base64 32
```

この鍵をローテーションすると既存の暗号化パスワードを復号できなくなります。
鍵はシークレットマネージャー（Kubernetes Secret、AWS Secrets Manager 等）に
保管してください。

## ローカル開発

詳細は [docs/development.md](docs/development.md) を参照してください。

```bash
# バックエンド
VANTYX_EXTERNAL_HOST=localhost \
  VANTYX_TLS_CERT_FILE=./certs/tls.crt \
  VANTYX_TLS_KEY_FILE=./certs/tls.key \
  go run ./cmd/vantyx-server

# フロントエンド
cd web && npm install && npm run dev
```

lint・テスト・カバレッジ：

```bash
make fmt    # gofmt + goimports + eslint --fix
make lint   # golangci-lint + eslint
make test   # go test ./... + go vet ./...
make e2e    # Playwright E2E（Docker 必須）
```

## セキュリティ

Vantyx は機密データの保存・通信について
[OWASP ASVS Level 2](docs/SECURITY-ASVS-L2.ja.md) を目標としています。
脆弱性報告は [SECURITY.ja.md](SECURITY.ja.md) を参照し、公開 Issue ではなく
GitHub Private Security Advisory フォームを使用してください。

## コントリビューション

バグ報告・機能リクエスト・コード・ドキュメント・翻訳を歓迎します。
[CONTRIBUTING.ja.md](CONTRIBUTING.ja.md) からお読みいただき、
[行動規範](CODE_OF_CONDUCT.ja.md) を遵守してください。変更履歴は
[CHANGELOG.md](CHANGELOG.md) で追跡します。

## ライセンス

Vantyx は [Apache License 2.0](LICENSE) で配布しています。サードパーティの
クレジットは [NOTICE](NOTICE) を参照してください。
