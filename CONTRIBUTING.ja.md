# Vantyx へのコントリビューションについて

[English](CONTRIBUTING.md)

Vantyx に関心をお寄せいただきありがとうございます。本書は Issue 起票・変更
提案・Pull Request の流れをまとめたものです。

参加にあたっては [行動規範](CODE_OF_CONDUCT.ja.md) を遵守してください。

---

## 目次

- [コントリビューションの種類](#コントリビューションの種類)
- [バグ報告とセキュリティ問題](#バグ報告とセキュリティ問題)
- [開発環境の準備](#開発環境の準備)
- [ブランチ運用](#ブランチ運用)
- [コミットメッセージ規約](#コミットメッセージ規約)
- [Pull Request チェックリスト](#pull-request-チェックリスト)
- [コーディング規約](#コーディング規約)
- [テスト](#テスト)
- [ドキュメント](#ドキュメント)
- [リリース](#リリース)

---

## コントリビューションの種類

- **バグ報告**: GitHub Issue（バグテンプレート）を使用してください。
- **機能リクエスト・提案**: GitHub Issue（feature テンプレート）を使用してください。
- **ドキュメント改善**: `docs/`、`README.md`、ソース内 godoc/JSDoc コメントへの
  PR は大歓迎です。
- **コード貢献**: バグ修正・リファクタリング・新機能・プロトコル追加・テスト追加など。

## バグ報告とセキュリティ問題

- **セキュリティ以外のバグ**: 再現手順・期待動作と実動作・バージョン情報
  （ビルド元コミット SHA 等）・関連ログを添えて GitHub Issue を起票してください。
- **脆弱性**: 公開 Issue は使わず、必ず [SECURITY.ja.md](SECURITY.ja.md) の
  手順で非公開に報告してください。

## 開発環境の準備

詳細は [docs/development.md](docs/development.md) を参照。手早く始めるには：

```bash
# バックエンド（自己署名証明書を ./certs に自動生成）
VANTYX_EXTERNAL_HOST=localhost \
  VANTYX_TLS_CERT_FILE=./certs/tls.crt \
  VANTYX_TLS_KEY_FILE=./certs/tls.key \
  go run ./cmd/vantyx-server

# フロントエンド（:5173、/api と /ws はバックエンドへプロキシ）
cd web && npm install && npm run dev
```

lint・テスト・カバレッジ：

```bash
make fmt    # gofmt + goimports + eslint --fix
make lint   # golangci-lint + eslint
make test   # go test ./... + go vet ./...
make e2e    # Playwright E2E テスト（docker 必須）
```

## ブランチ運用

- `main` は既定ブランチかつリリースブランチ。マージ前に CI を緑にしてください。
- `dev` は進行中の作業を集約するインテグレーションブランチ。`main` 向けの PR は
  `dev` から派生するか、`dev` の最新を取り込んだトピックブランチから出してください。
- トピックブランチ名: `feat/<scope>-<short-desc>`、`fix/<scope>-<short-desc>`、
  `docs/<scope>`、`refactor/<scope>`、`test/<scope>`、`chore/<scope>`。

## コミットメッセージ規約

Vantyx は [Conventional Commits 1.0](https://www.conventionalcommits.org/ja/) に
従います：

```
<type>(<optional scope>): <subject>

<optional body>

<optional footer(s)>
```

主な `type`：

| Type     | 使いどころ                                                       |
|----------|------------------------------------------------------------------|
| feat     | ユーザーから見える新機能                                         |
| fix      | バグ修正                                                          |
| docs     | ドキュメントのみ                                                  |
| refactor | 振る舞いを変えないコード変更                                      |
| perf     | パフォーマンス改善                                                |
| test     | テストの追加・改善                                                |
| build    | ビルド・依存関係・Docker・Makefile 変更                           |
| ci       | CI ワークフロー変更                                              |
| chore    | 上記に当てはまらないメンテナンス作業                              |
| revert   | 過去コミットの取り消し                                            |

type の後ろに `!` を付けるか、フッターに `BREAKING CHANGE:` と記載した場合は
破壊的変更とみなし、`CHANGELOG.md` にも記載してください。

## Pull Request チェックリスト

レビュー依頼前に下記を確認してください：

- [ ] PR タイトルが Conventional Commits に従っている。
- [ ] `make lint` と `make test` がローカルで通る。
- [ ] 新規・変更された動作をテストでカバーしている。
- [ ] 公開関数・型に英語の godoc / JSDoc コメントを書いている。
- [ ] 環境変数を追加・変更したら `docs/configuration.md` を更新した。
- [ ] ユーザーから見える変更を `CHANGELOG.md` の `Unreleased` セクションに記載した。
- [ ] 破壊的変更は PR 本文と changelog に明記した。

## コーディング規約

### Go

- 対象 Go バージョンは [`go.mod`](go.mod) を参照。
- `gofmt -s` で整形（`make fmt` が自動実行）。
- [`golangci-lint`](https://golangci-lint.run/) を [`configs/golangci.yml`](configs/golangci.yml) で実行。
  公開シンボルは godoc 必須、コメントは識別子名で始める（`Foo does ...`）。
- ロギング: `internal/logging`（slog ベース）のヘルパを使う。新規コードで
  `log.Printf` を直接呼ばないでください。
- エラー: `var ErrXxx = errors.New(...)` のセンチネル化を原則とし、呼び出し側で
  `errors.Is` / `errors.As` で分岐します。

### JavaScript / Web

- `web/src` は ESM。ランタイムでは TypeScript ビルドを行いません。
- ESLint で lint（`npm --prefix web run lint`、`make lint` でも実行）。
- export 関数には JSDoc を付与すると編集・レビュー時に役立ちます。
- ユーザー向け文字列は UI 全体と揃えて日本語のまま。構造的なコメントと
  識別子は英語を使ってください。

### コメント・ドキュメントの言語

- **ソースコード**（`internal/`、`cmd/`、`web/src/`）: 英語のみ。
- **公開ドキュメント**（`README.md`、`CONTRIBUTING.md`、`docs/*.md`、…）：
  英語を一次ドキュメントとし、日本語訳は `*.ja.md` として併設。

## テスト

- ユニットテスト: `go test ./...`。カバレッジしきい値は
  [`scripts/check_coverage.sh`](scripts/check_coverage.sh) で強制。
- E2E: [`e2e/README.md`](e2e/README.md) 参照。Playwright が Docker でサーバを
  起動し、SPA を操作します。
- テストは対象コードの近くに配置（`foo.go` と `foo_test.go`）。

## ドキュメント

- アーキテクチャ概要: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- 設定リファレンス: [`docs/configuration.md`](docs/configuration.md)
- セキュリティ（OWASP ASVS L2）: [`docs/SECURITY-ASVS-L2.md`](docs/SECURITY-ASVS-L2.md)
- API リファレンス: `/docs`（Swagger UI）と [`docs/api/openapi.yaml`](docs/api/openapi.yaml)

ユーザー向け挙動を変える場合、`README.md` と `docs/` 配下の該当ファイルを
更新してください。二言語ファイルを更新する場合は `*.md` と `*.ja.md` の両方
を更新します。難しい場合は PR で言及いただければメンテナがフォローします。

## リリース

Vantyx は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に従い
[`CHANGELOG.md`](CHANGELOG.md) を運用します。リリース手順：

1. `Unreleased` のエントリを新しい日付付きバージョンセクションに移動。
2. コミットに `vX.Y.Z`（Semantic Versioning）でタグ。
3. その changelog セクションを本文として GitHub Release を作成。

1.0 到達までは pre-release バージョニング（`0.y.z`）とし、`0.y` のバンプで
破壊的変更が入る可能性があります。
