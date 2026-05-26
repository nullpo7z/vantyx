# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
Until the `1.0.0` release, breaking changes may land in any `0.y` bump.

## [Unreleased]

### Changed

- CI: migrated `configs/golangci.yml` to the golangci-lint v2 schema
  (`version: "2"`, `linters.settings`, `linters.exclusions`) and bumped
  `golangci/golangci-lint-action` from v6 to v8 with `version` pinned to
  `v2.12.2`. `gosimple` is dropped from the enable list because v2
  merges it into `staticcheck`; the new `staticcheck` `ST1005` / `QF*`
  hints and the new `gosec` `G602` / `G703` taint analysers are
  silenced to keep the v1 lint surface. Necessary because
  `version: latest` started resolving to v2 and rejected the old v1
  config.
- Docs: rewrote `docs/SECURITY-ASVS-L2.md` as a concise best-effort
  security checklist instead of a full ASVS L2 self-assessment.
  Re-framed the security claims in `README.md` / `SECURITY.md` (and their
  Japanese counterparts) as "follows the spirit of ASVS L2" to avoid
  implying a formal audit.
- Docs: dropped the Japanese mirrors under `docs/`
  (`docs/ARCHITECTURE.ja.md`, `docs/SECURITY-ASVS-L2.ja.md`,
  `docs/roadmap.ja.md`). Top-level documents (`README`, `SECURITY`,
  `CONTRIBUTING`) keep their bilingual pairs; everything under `docs/` is
  English only to cap the bilingual-maintenance load.
- Docs: trimmed `docs/roadmap.md` to the open items only. The list of
  shipped features lives in `README.md` and was duplicated in the old
  roadmap; the new file just enumerates what is still missing so the
  two sources do not drift.
- CI: dropped the coverage threshold gate. `test-go` still computes
  coverage for the security-sensitive packages and prints the total
  for visibility, but does not fail on a numeric threshold. The
  `scripts/check_coverage.sh` helper and the `make coverage` target
  remain available locally (the Makefile target now accepts an
  optional `MIN=NN` to opt back in).
- Lint: trimmed `configs/golangci.yml` to a minimal personal-OSS set
  (`govet`, `errcheck`, `staticcheck`, `gosimple`, `unused`,
  `ineffassign`, `gosec`, `misspell`, `bodyclose`). The opinionated
  style linters (`revive` godoc requirements, `gocyclo`, `unparam`,
  `nilerr`) are off so PRs are not blocked on style nits unrelated to
  the change.
- Deps: `.github/dependabot.yml` now groups all minor/patch updates
  per ecosystem (gomod / npm web / npm e2e / github-actions / docker)
  into a single PR each, and lowers `open-pull-requests-limit` from
  10/10/5/—/— to 3/3/2/2/2. Major upgrades still open separate PRs.
- CI: split the workflow so per-PR runs only execute the fast gates
  (`lint-go`, `lint-fe`, `test-go`). The slower `link-check`,
  `security-scan` (Trivy + gosec), and `build-docker` jobs moved to a
  new `ci-weekly.yml` that runs on a weekly schedule and on
  `workflow_dispatch`. `lint-go` now uses
  `golangci/golangci-lint-action` so the linter binary is cached
  between runs instead of being `go install`-ed every time. `setup-go`
  / `setup-node` opt in to module / npm caching.

### Added

- OSS scaffolding: `LICENSE` (Apache-2.0), `NOTICE`, `CONTRIBUTING.md`
  (English + Japanese), `SECURITY.md`, `.github/` bug-report issue template,
  dependabot, and `.editorconfig`.
- Bilingual top-level documentation (`README.md` + `README.ja.md`,
  `SECURITY.md` + `SECURITY.ja.md`, `CONTRIBUTING.md` + `CONTRIBUTING.ja.md`).
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
