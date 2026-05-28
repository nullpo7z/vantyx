# Self-hosted GitHub Actions runner

CI workflows in `.github/workflows/` use labels `[self-hosted, linux, x64]`.
Install the tools below on the runner host **before** relying on CI, or ensure
`actions/setup-go` / `actions/setup-node` can download toolchains (requires
`tar`, `gzip`, and outbound HTTPS).

Versions match `go.mod`, `web/package.json`, and `.github/workflows/ci-dev.yml`.

## Required packages (OS)

Ubuntu / Debian example:

```bash
sudo apt-get update
sudo apt-get install -y \
  git curl ca-certificates tar gzip xz-utils \
  build-essential
```

## Go (required)

Minimum: **Go 1.26.3** (see `toolchain` in `go.mod`).

### Option A — official tarball (recommended on runners)

```bash
GO_VERSION=1.26.3
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tgz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.tgz
echo 'export PATH=/usr/local/go/bin:$PATH' | sudo tee /etc/profile.d/golang.sh
export PATH=/usr/local/go/bin:$PATH
go version
```

Ensure the `actions-runner` service user sees `/usr/local/go/bin` on `PATH`
(systemd `Environment=PATH=...` or login shell that sources `/etc/profile.d/golang.sh`).

### Option B — `actions/setup-go` only

If Go is not pre-installed, `setup-go` downloads it. That step unpacks archives
with `tar`; install `tar` and `gzip` first or checkout/actions will fail with
`Can't use 'tar -xzf'`.

## Node.js (required for `lint-fe`)

Minimum: **Node.js 22.13.0** (`web/package.json` `engines`).

```bash
# Example with NodeSource (adjust for your distro policy)
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
sudo apt-get install -y nodejs
node -v   # v22.13.x or newer
```

Or install via `nvm` / `fnm` for the user that runs the Actions runner.

## Optional but useful

| Tool | Used by |
|------|---------|
| `golangci-lint` v2.12.2 | `lint-go` (installed on first CI run if missing) |
| `docker` | `ci-weekly.yml` `build-docker` |
| `docker buildx` | same |

Pre-install golangci-lint to speed up CI:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
# $(go env GOPATH)/bin must be on PATH for the runner user
```

## Register the runner

Follow GitHub’s guide for your repo or organization. Example labels:

- `self-hosted`
- `linux`
- `x64`

Workflows expect exactly: `runs-on: [self-hosted, linux, x64]`.

## Quick sanity check (as the runner user)

```bash
go version          # go1.26.3 ...
node -v             # v22.13.0 or newer
npm -v
tar --version
git --version
docker --version    # optional unless running weekly workflow
```

## Troubleshooting

| Symptom | Likely cause |
|---------|----------------|
| `Can't use 'tar -xzf'` when checking out or running `setup-go` | Missing `tar` / `gzip` on the runner host |
| `go: command not found` after `setup-go` | Go not on `PATH` for the runner service user; pre-install Go or fix systemd `PATH` |
| `golangci-lint` install fails mid-download | Network flake; retry CI or pre-install with `go install` |
| `migrate: context deadline exceeded` in tests | Slow disk; set `VANTYX_SQLITE_MIGRATE_TIMEOUT=2m` on the runner |
