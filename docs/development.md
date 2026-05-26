# Development guide

This guide complements [`CONTRIBUTING.md`](../CONTRIBUTING.md). It is the
go-to reference for running Vantyx locally, testing it, and following the
project's coding conventions.

## Prerequisites

- Go pinned by [`go.mod`](../go.mod) (`go.mod` reports the minimum
  toolchain version).
- Node.js 20 LTS or newer for the frontend.
- Docker / docker compose for the integration and end-to-end tests.
- `make` for the documented developer commands.
- `golangci-lint` for running the Go lint suite locally (also invoked
  by `make lint-go`). Install the same v1 line CI uses with:

  ```bash
  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
  # ensure $(go env GOPATH)/bin is on your PATH
  ```

  Then `golangci-lint run --config=./configs/golangci.yml --issues-exit-code=1`
  reproduces the CI lint step verbatim.

## Repository layout

| Path | Purpose |
|------|---------|
| [`cmd/vantyx-server`](../cmd/vantyx-server) | The single binary that boots HTTP/HTTPS listeners, optional CLI SSH gateway, and the embedded TFTP server. |
| [`internal/access`](../internal/access) | Target / access-group store and tag-based access control. |
| [`internal/auth`](../internal/auth) | Local password authentication, SSH key registration, and session store. |
| [`internal/db`](../internal/db) | SQLite driver wrapper and migrations. |
| [`internal/filetransfer`](../internal/filetransfer) | Background upload / download job manager. |
| [`internal/ftp`](../internal/ftp), [`internal/sftp`](../internal/sftp), [`internal/tftp`](../internal/tftp) | Per-protocol file-transfer clients (and the embedded TFTP server). |
| [`internal/httpapi`](../internal/httpapi) | REST API, WebSocket handlers, middleware, audit pipeline. |
| [`internal/logging`](../internal/logging) | Structured logging and audit helpers built on `log/slog`. |
| [`internal/protocols`](../internal/protocols) | Capability matrix for `(protocol, feature)` lookups. |
| [`internal/rdpvnc`](../internal/rdpvnc), [`internal/vncproxy`](../internal/vncproxy) | RDP-to-VNC bridge and VNC WebSocket proxy. |
| [`internal/recording`](../internal/recording) | asciinema cast writer. |
| [`internal/secret`](../internal/secret) | AES-256-GCM encryption for sensitive fields. |
| [`internal/session`](../internal/session) | Terminal session manager (resume / detach / idle warnings). |
| [`internal/sshd`](../internal/sshd) | CLI SSH gateway (`ssh user@vantyx`). |
| [`internal/sshproxy`](../internal/sshproxy), [`internal/telnetproxy`](../internal/telnetproxy) | SSH / Telnet proxy bridges. |
| [`web/src`](../web/src) | SPA built on vanilla ES modules + Tailwind. |
| [`e2e`](../e2e) | Playwright end-to-end test suite. |
| [`docs`](.) | Architecture, security, configuration, and roadmap docs. |

## Running locally

### Backend

```bash
VANTYX_EXTERNAL_HOST=localhost \
  VANTYX_TLS_CERT_FILE=./certs/tls.crt \
  VANTYX_TLS_KEY_FILE=./certs/tls.key \
  go run ./cmd/vantyx-server
```

- The backend will auto-generate a self-signed certificate the first
  time it runs (into `./certs/`).
- Default ports: HTTP `:8080` → HTTPS redirect, HTTPS `:8443`.
- SQLite lives at `data/vantyx.db` unless `VANTYX_SQLITE_PATH` is set.

### Frontend (Vite dev server)

```bash
cd web
npm install
npm run dev
```

- Serves on `http://localhost:5173`.
- Vite proxies `/api/*` and `/ws/*` to the backend.

### Single-container Docker

```bash
docker compose up --build
```

`docker-compose.yml` maps host ports `80/443/69` to the container, mounts
a named volume for certs and the SQLite directory, and exports a small
set of `VANTYX_*` env vars.

## Tests

| Command | Scope |
|---------|-------|
| `make test` | `go vet ./...` + `go test ./...`. |
| `make coverage` | `go test ./... -covermode=atomic -coverprofile=coverage.out` plus the threshold check. |
| `make e2e` | Playwright suite under `e2e/`. Requires Docker. |
| `make smoke` | Build the Docker image and hit `/healthz`. |

The coverage threshold is enforced by
[`scripts/check_coverage.sh`](../scripts/check_coverage.sh) and applied in
CI to a subset of the most security-sensitive packages.

End-to-end tests are documented in [`e2e/README.md`](../e2e/README.md).

## Coding conventions

### Go

- Format with `gofmt -s -w .` (run via `make fmt`). The project also
  uses [`goimports`](https://pkg.go.dev/golang.org/x/tools/cmd/goimports)
  to normalise import groups.
- Lint with `golangci-lint` using
  [`configs/golangci.yml`](../configs/golangci.yml). Mandatory
  linters include `revive` (godoc on every exported symbol),
  `gocyclo`, `gosec`, `errcheck`, `gosimple`, `govet`, `staticcheck`,
  `misspell`, and `unparam`.
- Public symbols must have godoc comments starting with the symbol
  name (`Foo does ...`). Package documentation lives in a `doc.go`
  file per package.
- Logging: use the helpers in
  [`internal/logging`](../internal/logging). Do not call `log.Printf`
  directly in new code.
- Errors: define sentinel errors (`var ErrXxx = errors.New(...)`) in
  the package that owns the failure, and let HTTP handlers branch with
  `errors.Is` / `errors.As`. The HTTP handlers translate these into
  RFC-friendly status codes.
- Avoid global mutable state. When a test needs to swap behaviour, add
  an injection seam (function variable) rather than reaching across
  packages.

### JavaScript

- `web/src` is plain ESM. No TypeScript runtime.
- Format and lint via `npm --prefix web run lint` (also wired into
  `make lint`).
- ESLint config: [`web/eslint.config.js`](../web/eslint.config.js).
  `no-unused-vars` is `error`; JSDoc on exported helpers is encouraged.
- DOM updates flow through small, page-specific modules (one file per
  page). Shared helpers live alongside in `web/src/`.

## Logging keys

All log lines emitted through `internal/logging` use the following
canonical keys. New events should reuse them whenever possible so log
queries stay consistent.

| Key | Meaning |
|-----|---------|
| `event` | Short snake_case event name (for audit lines). |
| `user_id` | Vantyx user identifier (string). |
| `username` | Human-readable username (string). |
| `target_id` | Target identifier (string). |
| `session_id` | Terminal / RDP / VNC session identifier (string). |
| `protocol` | `ssh`, `telnet`, `vnc`, `rdp`, `sftp`, `ftp`, or `tftp`. |
| `method` | HTTP method (`GET`, `POST`, …). |
| `path` | HTTP path. |
| `status` | HTTP status code (int). |
| `remote` | Remote address (string). |
| `duration_ms` | Elapsed time in milliseconds (int). |
| `query` | URL query (string). |
| `user_agent` | `User-Agent` header (string). |
| `content_type` | `Content-Type` header (string). |
| `error` | Error message (string). Never include stack traces. |

Add domain-specific keys (`recording_id`, `transfer_id`, `host`, …) as
needed; pick the most specific name and document it next to the
emitting code.

## Releases

Releases follow [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/). To cut one:

1. Move the entries under `Unreleased` in [`CHANGELOG.md`](../CHANGELOG.md)
   into a new dated `X.Y.Z` section.
2. Tag the commit `vX.Y.Z`.
3. Draft a GitHub Release with the changelog section as the description.

Until `1.0.0`, the project uses `0.y.z` pre-release versioning and breaking
changes can land in any `0.y` bump.
