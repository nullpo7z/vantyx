# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
Until the `1.0.0` release, breaking changes may land in any `0.y` bump.

## [Unreleased]

### Security

- Add: centralized `SecurityHeadersMiddleware` on all HTTP routes — CSP,
  Permissions-Policy, COOP, CORP, COEP (`unsafe-none` for noVNC/wasm),
  Referrer-Policy, `Cache-Control: no-store`, and HSTS on HTTPS
  (`internal/httpapi/security_headers.go`; replaces ad-hoc headers in
  `cmd/vantyx-server/main.go`).
- Add: OWASP ZAP wrapper scripts under `scripts/zap/` (baseline, authenticated
  baseline, OpenAPI, full scan, session cookie login helper, rule overrides).
- Fix: `force_password_change` now blocks `/ws/*` handshakes as well as
  `/api/*`, so initial-password rotation cannot be bypassed via terminal
  / RDP / VNC WebSockets (CWE-1188).
- Fix: CLI SSH gateway (`sshd`) shares the HTTP login rate limiter
  (`internal/ratelimit`) for per-IP and per-username brute-force throttling.
- Fix: invitation `RecordUse` uses a conditional UPDATE so concurrent joins
  cannot exceed `max_uses`.
- Fix: per-target `SSHHostKeyInsecureSkipVerify` is wired into SSH bridge
  / SFTP connections via `WithTargetInsecureSkipVerify`.
- Fix: target host validation blocks loopback, link-local, and cloud
  metadata IPs by default (`VANTYX_ALLOW_RESTRICTED_HOSTS=1` to override).
- Fix: WebSocket credential errors return localized messages instead of raw
  `err.Error()` (ASVS V8.1).
- Fix: CSRF middleware requires same-origin Origin/Referer on cookie-less
  unsafe `/api/*` requests (except `/api/login`).
- Fix: command-log redaction covers additional password-bearing CLI patterns
  (`curl -u`, `-P`, `--pass`, etc.).
- Fix: production `docker-compose.yml` adds container hardening
  (`no-new-privileges`, `cap_drop`, `read_only`, `tmpfs`).
- Fix: BroadcastChannel credential hand-off checks `event.origin`; invite
  join URLs are cached in memory instead of `sessionStorage`.

### Removed

- Playwright end-to-end test suite (`e2e/`), `docker-compose.e2e.yml`,
  `scripts/run-e2e-*.sh`, and the `make e2e` target.

### Added

- Feature: collaborative session sharing for VNC and RDP — session list/join
  APIs, invitation options, SSH CLI sharing commands, and web UI
  (`sharing_ui.js`, terminal/VNC/RDP page integration).
- Feature: each row in the issued-invitations table now offers
  **Show link** (reuses the URL from this browser when still valid),
  **Reissue** (POST `.../invitations/{id}/join-url` rotates the token),
  and **Delete** (revoke). New links invalidate the previous token.
- Fix: the invite-management dialog now refreshes its status column
  in real time. It shares the `/api/events/sessions` SSE stream with
  the terminal page and reloads when invitations are consumed or
  revoked; a 30-second timer also picks up time-based expirations.
  The backend emits a new `invitation_consumed` sharing event when a
  join link is used.
- Fix: the issued-invitations table treated API `expires_at` values
  (Unix seconds) as JavaScript milliseconds, so unused invites showed
  1970-era expiry times and were always labelled expired. The list
  now parses server timestamps correctly, renames the expiry column
  to "Expires" / "有効期限", and uses a dedicated "Status" /
  "ステータス" column (revoke stays under "Actions" / "操作").
- Fix: opening a collaborative-session invitation URL while logged
  out sent the user to the login page but, after signing in, always
  landed on the home screen instead of returning to
  `/terminal?session_id=…&mode=viewer&invite=…`. Standalone pages now
  stash the requested path (and `?next=` on the login link) in
  `sessionStorage` and navigate back once authentication succeeds.
- Terminal page header: owners of an active SSH/Telnet session now
  see an **Invitations** button that opens the same invite-management
  dialog as the sessions list (issue named or link invitations, copy
  join URLs, revoke pending invites). Hidden for viewers and until a
  `session_id` is established.
- Fix: the Edit Target modal's "SSH host key" section was always
  displaying "Not registered" even when a fingerprint was on file.
  The modal builds its target stub from the `data-target-*`
  attributes of the edit button, but the button never serialised
  `ssh_host_key_fingerprint`, so the modal had nothing to show.
  The button now exposes `data-target-ssh-host-key-fp` and the
  modal also refreshes the value via `GET /api/targets` after
  rendering, so adoption performed in another tab (e.g. through
  the terminal-page TOFU dialog) is reflected immediately. The
  Clear button is now disabled when there is nothing to clear, and
  the "Not registered" placeholder copy was reworded from "(will
  confirm on first connect)" to the more accurate "(will be
  prompted in the adoption dialog on connect)".
- Fix: when the SSH bridge fails host-key verification on the
  initial dial, the terminal page no longer renders the misleading
  "session is still running on the backend" banner alongside the
  credentials form. The host-key TOFU / mismatch dialog now hides
  every other transient wrap on entry, and dismissing it lands on
  a single clear "verification cancelled — connection cannot be
  established" banner that explains the next step. Each connect-flow
  `ws.onclose` handler also short-circuits when a host-key error has
  fired so it does not race the dialog with stale recovery UI.
  Additionally, the WebSocket that delivered the `host_key_unknown` /
  `host_key_mismatch` frame now has its `onmessage` / `onclose` /
  `onerror` / `onopen` handlers detached before `ws.close()` is
  called. Without that, a delayed close-handshake from the old
  socket could fire after the user adopted the key and a fresh
  socket had already attached, writing a stale "[connection
  closed]" line into the live xterm and re-surfacing the
  disconnected banner over the working session.
- Fix: `POST /api/targets/probe-host-key` was forcing
  `HostKeyAlgorithms` to ED25519-first, but the detachable SSH
  bridge does not set the field and therefore inherits Go
  `x/crypto/ssh`'s default order which prefers RSA over ED25519.
  On servers that advertise multiple host keys (the OpenSSH
  default), the probe captured the ED25519 fingerprint while the
  bridge later negotiated the RSA key, leaving operators stuck in
  an adopt → mismatch → re-adopt loop. The probe now uses the
  same default order so probe and bridge always converge on the
  same key.
- SSH host key fingerprint UI / API + Trust-On-First-Use (TOFU)
  adoption flow. Two admin-only endpoints back the new flow:
  `POST /api/targets/probe-host-key` performs a one-shot SSH dial
  and returns the offered SHA-256 fingerprint without persisting
  anything; `PUT /api/targets/{id}/ssh-host-key` adopts (or clears
  with empty body) the fingerprint and emits a dedicated audit
  event. The Add Target modal now auto-probes the upstream host
  when SSH is selected and asks for explicit confirmation before
  recording the fingerprint, the Edit Target modal exposes
  re-fetch and clear buttons, and the terminal page surfaces a
  TOFU adoption dialog (and a stronger checkbox-gated mismatch
  warning) when the bridge reports `host_key_unknown` or
  `host_key_mismatch` via a structured WebSocket frame. After the
  user accepts, the cached credentials drive an automatic
  reconnect. New audit events: `target_host_key_probed`,
  `target_host_key_adopted`, `target_host_key_cleared`,
  `target_host_key_probe_failed`, `terminal_host_key_unknown_presented`,
  `terminal_host_key_mismatch_presented`. Also fixes a missing
  SQLite migration for `ssh_host_key_insecure_skip_verify` that
  prevented brand-new deployments from ever calling
  `SetSSHHostKeyInsecureSkipVerify`.
- Collaborative terminal sessions (Phase A: SSH/Telnet). A session
  owner can invite other Vantyx users either by name or via a
  one-shot share link, and invitees attach to the running bridge as
  read-only viewers. Exactly one writer holds input control at a
  time; viewers can request control and the current writer accepts
  or refuses through a modal. Invitations are persisted in
  `session_invitations` (only the SHA-256 hash of the token is
  stored) and expire after a configurable TTL. New REST endpoints
  under `/api/terminal/sessions/{id}` cover invitation issue / list /
  revoke, join, participants list / kick, and write-token
  request / grant / deny / release. The detachable SSH and Telnet
  bridges fan-out output to every attached client, drop viewer
  stdin server-side, and switch the writer without disconnecting
  anyone. New audit events (`session_invitation_*`,
  `session_participant_*`, `session_write_*`) record every
  collaboration action.

### Changed

- Toolchain: Go 1.27 (`toolchain go1.27.1`, `golang:1.27-alpine` builder),
  runtime image `alpine:3.24`, golangci-lint v2.13.2.
- Dependencies: `modernc.org/sqlite` 1.58.0, `go-oidc` 3.21.0, `chi` 5.3.2,
  `pkg/sftp` 1.13.11, `jlaffaye/ftp` 0.2.4, `x/oauth2` 0.37.0; npm
  `asciinema-player` 3.17.0, `tailwindcss` 4.3.3, `eslint` 10.10.0.
- Docker: drop `su-exec` from the runtime image and start the
  process directly as the non-root user via `USER nonroot:nonroot`
  in the Dockerfile. The previous entrypoint relied on `su-exec`
  to demote root → nonroot at start-up, which fails with
  `setgroups: Operation not permitted` when the container runs
  with `cap_drop: ALL` (e.g. on a Proxmox unprivileged LXC host).
  The hardening posture in `docker-compose.yml` is unchanged and
  the container still cannot regain root inside; it simply never
  starts as root in the first place. Bind-mounted host paths must
  be writable by UID 65532.
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
  per ecosystem (gomod / npm web / github-actions / docker)
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
- `Makefile` reorganised with `fmt`, `lint`, `test`, `coverage`,
  and `smoke` targets.

### Removed

- Internal design drafts under `docs/plan/` moved out of the public tree.

### Notes

- This is the first publicly released version. Breaking changes since the
  pre-release internal development branch are not catalogued individually.
