# Development guide

This guide complements [`CONTRIBUTING.md`](../CONTRIBUTING.md). It is the
go-to reference for running Vantyx locally, testing it, and following the
project's coding conventions.

## Prerequisites

- Go pinned by [`go.mod`](../go.mod) (`go.mod` reports the minimum
  toolchain version).
- Node.js **22.13.0 or newer** for the frontend (`web/scripts/check-node.mjs` enforces this; see `web/.nvmrc`).
- Docker / docker compose for smoke tests and containerized local deployments.
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
| [`docs`](.) | Architecture, security, configuration, credentials, collaborative sessions, and roadmap. |

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
npm install   # requires Node >= 22.13.0 (nvm use)
npm run dev
```

- Serves on `http://localhost:5173`.
- Vite proxies `/api/*` and `/ws/*` to the backend.

### Docker (operations — published image)

Operators use [docker-compose.yml](../docker-compose.yml) (pull only, no `build`):

```bash
cp .env.example .env   # set VANTYX_EXTERNAL_HOST and encryption key
# Edit /path/to/vantyx in docker-compose.yml; create host directories
mkdir -p /path/to/vantyx/{certs,data,recordings}
docker compose pull
docker compose up -d
```

Image: `nullpo7z/vantyx:latest` (pinned in `docker-compose.yml`; change the tag there if needed).

### Docker (development — build from source)

Contributors use [docker-compose.dev.yml](../docker-compose.dev.yml):

```bash
cp .env.example .env
docker compose -f docker-compose.dev.yml up --build -d
```

The local image is tagged `nullpo7z/vantyx:dev` so it does not replace the
published `:latest` tag on your machine.

Both compose files map host ports `80/443/2222/69`, bind-mount
`/path/to/vantyx/{certs,data,recordings}` (edit before first start), and load
`VANTYX_*` variables from `.env`.

**Production vs development:** [`docker-compose.yml`](../docker-compose.yml)
pulls `nullpo7z/vantyx:latest` and sets only the env vars required for a
minimal deployment. [`docker-compose.dev.yml`](../docker-compose.dev.yml)
builds the image locally, tags it `:dev`, and adds dev-oriented settings
(`read_only`, `cap_drop`, `no-new-privileges`, explicit `VANTYX_SQLITE_PATH`,
TFTP paths, etc.). Use the dev file for hacking on the tree; use the root
file for production-style runs.

## Git branches and releases

| Branch | Purpose |
|--------|---------|
| **`dev`** | Daily integration. Commit and push feature work here. CI runs on every push. |
| **`main`** | Production. Tracks Docker Hub `nullpo7z/vantyx:latest`. **Never push directly.** |
| **`feat/*`, `fix/*`** | Optional short-lived branches for large or isolated work; open a PR into **`dev`**. |

**Typical flow**

1. Develop on `dev` (or branch off `dev`, then merge back via PR).
2. When ready to release, open a PR **`dev` → `main`**, wait for CI, merge.
3. After `main` is updated, build and push the Docker image from `main` (`:latest` and the short commit SHA tag).

Do not merge feature branches directly into `main`; land on `dev` first so integration history stays linear.

## Tests

| Command | Scope |
|---------|-------|
| `make test` | `go vet ./...` + `go test ./...`. |
| `make coverage` | Runs the focused coverage suite for the security-sensitive packages and prints the total. Informational only — pass `MIN=NN` to fail the target when the total drops below NN percent. |
| `make smoke` | Build the Docker image and hit `/healthz`. |

Coverage is not a CI gate: PRs are not blocked on a numeric threshold.
[`scripts/check_coverage.sh`](../scripts/check_coverage.sh) is still
available locally for anyone who wants to enforce one
(`./scripts/check_coverage.sh coverage.out 75`, etc.).

## Coding conventions

### Go

- Format with `gofmt -s -w .` (run via `make fmt`). The project also
  uses [`goimports`](https://pkg.go.dev/golang.org/x/tools/cmd/goimports)
  to normalise import groups.
- Lint with `golangci-lint` using
  [`configs/golangci.yml`](../configs/golangci.yml). The enabled set
  is intentionally small: `govet`, `errcheck`, `staticcheck`,
  `gosimple`, `unused`, `ineffassign`, `gosec`, `misspell`,
  `bodyclose`. Opinionated style linters (`revive` godoc requirements,
  `gocyclo`, `unparam`, `nilerr`) are off.
- Godoc on exported symbols is encouraged but not enforced. When you
  do write one, follow the convention of starting it with the symbol
  name (`Foo does ...`). Package documentation can live in a `doc.go`
  file when useful.
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
