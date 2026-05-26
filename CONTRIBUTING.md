# Contributing to Vantyx

[日本語](CONTRIBUTING.ja.md)

Thanks for your interest! This is a small personal OSS project, so the rules
are intentionally lightweight.

## Reporting issues

- **Bugs**: open a GitHub Issue using the bug template, including reproduction
  steps and the version / commit you built from.
- **Security vulnerabilities**: please do **not** open a public issue. Follow
  [SECURITY.md](SECURITY.md) and report privately via the GitHub security
  advisory form.

## Development

See [docs/development.md](docs/development.md) for the full setup. Quick
checks before sending a PR:

```bash
make fmt    # gofmt + goimports + eslint --fix
make lint   # golangci-lint + eslint
make test   # go test ./... + go vet ./...
```

## Pull requests

- The default target branch is `dev`; `main` is the release branch.
- Commit messages and PR titles follow
  [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`,
  `fix:`, `docs:`, `refactor:`, `chore:`, …). Use `!` or `BREAKING CHANGE:`
  for breaking changes.
- Please add or update tests for behaviour changes, and note user-visible
  changes in `CHANGELOG.md` (Unreleased).
- Source code comments and identifiers are English; user-facing UI strings
  match the existing locale catalogs in `internal/i18n/locales/`.

That's it — feel free to open a draft PR early if you want feedback.
