# Contributing to Vantyx

[日本語](CONTRIBUTING.ja.md)

Thank you for your interest in contributing to Vantyx! This document explains how
to file issues, propose changes, and submit pull requests.

By participating you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).

---

## Table of contents

- [Ways to contribute](#ways-to-contribute)
- [Reporting bugs and security issues](#reporting-bugs-and-security-issues)
- [Development setup](#development-setup)
- [Branching model](#branching-model)
- [Commit message convention](#commit-message-convention)
- [Pull request checklist](#pull-request-checklist)
- [Coding style](#coding-style)
- [Tests](#tests)
- [Documentation](#documentation)
- [Releases](#releases)

---

## Ways to contribute

- **Bug reports**: open a GitHub Issue using the bug template.
- **Feature requests / proposals**: open an Issue using the feature template.
- **Documentation improvements**: PRs to anything under `docs/`, `README.md`, or
  inline godoc/JSDoc comments are very welcome.
- **Code contributions**: bug fixes, refactors, new features, additional protocol
  support, or tests.

## Reporting bugs and security issues

- **Non-security bugs**: open a GitHub Issue with reproduction steps, expected
  vs. actual behaviour, version (`vantyx-server --version` once available, or the
  commit SHA you built from), and relevant log lines.
- **Security vulnerabilities**: please do **not** open a public issue. Follow
  [SECURITY.md](SECURITY.md) to report privately.

## Development setup

See [docs/development.md](docs/development.md) for a full walkthrough. Quick
start:

```bash
# Backend (uses self-signed TLS under ./certs)
VANTYX_EXTERNAL_HOST=localhost \
  VANTYX_TLS_CERT_FILE=./certs/tls.crt \
  VANTYX_TLS_KEY_FILE=./certs/tls.key \
  go run ./cmd/vantyx-server

# Frontend (port 5173, proxies /api and /ws to the backend)
cd web && npm install && npm run dev
```

Lint, test, and coverage:

```bash
make fmt    # gofmt + goimports + eslint --fix
make lint   # golangci-lint + eslint
make test   # go test ./... + go vet ./...
make e2e    # Playwright end-to-end tests (requires docker)
```

## Branching model

- `main` is the default and release branch. CI must be green before merging.
- `dev` is the integration branch for in-flight work. PRs targeting `main`
  should originate from `dev` or from a feature branch rebased on top of `dev`.
- Topic branches: `feat/<scope>-<short-desc>`, `fix/<scope>-<short-desc>`,
  `docs/<scope>`, `refactor/<scope>`, `test/<scope>`, `chore/<scope>`.

## Commit message convention

Vantyx follows [Conventional Commits 1.0](https://www.conventionalcommits.org/):

```
<type>(<optional scope>): <subject>

<optional body>

<optional footer(s)>
```

Common `type` values:

| Type     | When to use it                                                    |
|----------|-------------------------------------------------------------------|
| feat     | A user-visible new capability                                     |
| fix      | A bug fix                                                          |
| docs     | Documentation only                                                |
| refactor | Code change with no behaviour change                              |
| perf     | Performance improvement                                           |
| test     | Adding or refining tests                                          |
| build    | Build system / dependency / Docker / Makefile changes             |
| ci       | CI workflow changes                                               |
| chore    | Maintenance work that does not fit the above                      |
| revert   | Reverts a previous commit                                         |

A `!` after the type or `BREAKING CHANGE:` in the footer flags a breaking
change and should also appear in `CHANGELOG.md`.

## Pull request checklist

Before requesting review, please confirm:

- [ ] The PR title follows Conventional Commits.
- [ ] `make lint` and `make test` pass locally.
- [ ] New or changed behaviour is covered by tests.
- [ ] Public functions and types have godoc / JSDoc comments in English.
- [ ] `docs/configuration.md` is updated when adding or renaming environment
      variables.
- [ ] `CHANGELOG.md` `Unreleased` section is updated for user-visible changes.
- [ ] Breaking changes are clearly called out in the PR body and changelog.

## Coding style

### Go

- Target the Go version pinned in [`go.mod`](go.mod).
- Format with `gofmt -s` (run via `make fmt`).
- Lint with [`golangci-lint`](https://golangci-lint.run/) using
  [`configs/golangci.yml`](configs/golangci.yml). All public symbols must carry
  godoc comments and start with the identifier name (`Foo does ...`).
- Logging: use the helpers in `internal/logging` (slog-based). Do not call
  `log.Printf` directly in new code.
- Errors: define sentinel errors (`var ErrXxx = errors.New(...)`) in the
  package that owns the failure, and let callers branch with `errors.Is` /
  `errors.As`.

### JavaScript / Web

- ESM modules under `web/src`. No build-time TypeScript in the runtime bundle.
- Lint with ESLint via `npm --prefix web run lint` (also enforced by `make lint`).
- Add JSDoc to exported helpers; types help editors and code reviews.
- Keep user-facing strings in Japanese to match the rest of the UI; structural
  comments and identifiers must be in English.

### Comments and documentation language

- **Source code** (`internal/`, `cmd/`, `web/src/`): English only.
- **Top-level public docs** (`README.md`, `CONTRIBUTING.md`, `docs/*.md`,
  …): English primary, Japanese translations live in matching `*.ja.md` files.

## Tests

- Unit tests: `go test ./...`. Coverage gate is enforced by
  [`scripts/check_coverage.sh`](scripts/check_coverage.sh).
- End-to-end: see [`e2e/README.md`](e2e/README.md). Playwright spins up the
  server in Docker and exercises the SPA.
- Add tests close to the code they cover (`foo.go` next to `foo_test.go`).

## Documentation

- Architecture overview: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
- Configuration reference: [`docs/configuration.md`](docs/configuration.md).
- Security baseline (OWASP ASVS L2):
  [`docs/SECURITY-ASVS-L2.md`](docs/SECURITY-ASVS-L2.md).
- API reference: served from `/docs` (Swagger UI) and
  [`docs/api/openapi.yaml`](docs/api/openapi.yaml).

If your change affects user-visible behaviour, update both `README.md` and the
relevant document under `docs/`. When updating a bilingual document, please
update both `*.md` and `*.ja.md`; if you cannot, note it in the PR and a
maintainer will follow up.

## Releases

Vantyx tracks [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) under
[`CHANGELOG.md`](CHANGELOG.md). Maintainers cut releases by:

1. Moving entries out of `Unreleased` into a new dated version section.
2. Tagging the commit with `vX.Y.Z` (semantic version).
3. Drafting a GitHub Release with the changelog section as the description.

Until 1.0 is reached, the project follows pre-release versioning (`0.y.z`) and
breaking changes may land in any `0.y` bump.
