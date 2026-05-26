# Vantyx へのコントリビューションについて

[English](CONTRIBUTING.md)

個人開発の小規模 OSS なので、ルールは意図的に最小限にしています。
お気軽にどうぞ。

## Issue 報告

- **バグ**: バグテンプレートを使った GitHub Issue を起票してください。
  再現手順とビルド元のバージョン / コミット SHA を添えてください。
- **脆弱性**: 公開 Issue にせず、[SECURITY.ja.md](SECURITY.ja.md) を参照して
  GitHub Private Security Advisory から非公開で報告してください。

## 開発

詳細は [docs/development.md](docs/development.md) を参照。PR 前に実行すると
よいチェック：

```bash
make fmt    # gofmt + goimports + eslint --fix
make lint   # golangci-lint + eslint
make test   # go test ./... + go vet ./...
```

## Pull Request

- PR 先は `dev` ブランチがデフォルト（`main` はリリースブランチ）。
- コミットメッセージ / PR タイトルは
  [Conventional Commits](https://www.conventionalcommits.org/ja/) に従って
  ください（`feat:` / `fix:` / `docs:` / `refactor:` / `chore:` …）。
  破壊的変更は `!` または `BREAKING CHANGE:` を付けてください。
- 振る舞いを変える場合はテストの追加 / 更新と、ユーザー向け変更を
  `CHANGELOG.md`（Unreleased）に追記してください。
- ソースコード内のコメント・識別子は英語、UI 文字列は
  `internal/i18n/locales/` の既存カタログに合わせてください。

困ったらドラフト PR で先に相談してもらって構いません。
