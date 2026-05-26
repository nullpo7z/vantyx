# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
Until the `1.0.0` release, breaking changes may land in any `0.y` bump.

## [Unreleased]

### Added

- OSS scaffolding: `LICENSE` (Apache-2.0), `NOTICE`, `CONTRIBUTING.md`
  (English + Japanese), `SECURITY.md`, `.github/` bug-report issue template,
  dependabot, and `.editorconfig`.
- Bilingual top-level documentation (`README.md` + `README.ja.md`,
  `docs/ARCHITECTURE.md` + `docs/ARCHITECTURE.ja.md`,
  `docs/SECURITY-ASVS-L2.md` + `docs/SECURITY-ASVS-L2.ja.md`).
- Centralised configuration reference at `docs/configuration.md`.
- Developer guide at `docs/development.md` covering toolchain, tests,
  coverage, and logging key conventions.
- `internal/logging` package providing `slog`-based component loggers and a
  unified audit event sink.

### Changed

- HTTP request and audit logs migrated from `log.Printf` to structured `slog`
  output. Logging key names follow the convention documented in
  `docs/development.md`.
- Large source files split for readability:
  `internal/httpapi/router.go`, `internal/httpapi/file_transfers.go`,
  `internal/httpapi/terminal.go`, `internal/sshd/server.go`,
  and `web/src/app.js`.
- `golangci-lint` configuration tightened to require godoc on every public
  symbol and to enforce cyclomatic complexity, `errcheck`, `gosimple`,
  `staticcheck`, `misspell`, and `unparam`.
- `web/eslint.config.js` tightened with `no-unused-vars` errors and the
  jsdoc plugin.
- `Makefile` reorganised with `fmt`, `lint`, `test`, `e2e`, `coverage`,
  and `smoke` targets.

### Removed

- Internal design drafts under `docs/plan/` moved out of the public tree.

### Notes

- This is the first publicly released version. Breaking changes since the
  pre-release internal development branch are not catalogued individually.
