# Configuration reference

All runtime knobs live under the `VANTYX_*` namespace. This file is the
single source of truth for them; if you add or rename one, update this
file and `CHANGELOG.md` in the same pull request.

Naming convention: `VANTYX_<SUBSYSTEM>_<NAME>`.

## TLS / HTTP listener

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_HTTPS_ADDR` | `:8443` | Address the TLS listener binds to inside the container. Docker maps this to host `443`. |
| `VANTYX_HTTP_REDIRECT_ADDR` | `:8080` | Address the HTTP-to-HTTPS redirector binds to. Docker maps this to host `80`. |
| `VANTYX_HTTPS_READ_TIMEOUT_SEC` | `15` | `http.Server.ReadTimeout` in seconds. Increase to tolerate slow uploads. |
| `VANTYX_SHUTDOWN_TIMEOUT_SEC` | `10` | Graceful-shutdown deadline in seconds. |
| `VANTYX_TLS_CERT_FILE` | `/app/certs/tls.crt` | PEM certificate path. Auto-generated as a self-signed cert if missing. |
| `VANTYX_TLS_KEY_FILE` | `/app/certs/tls.key` | PEM private-key path. Auto-generated alongside the cert. |
| `VANTYX_TLS_SANS` | — | Comma-separated extra SANs for the self-signed cert (DNS names and IPs). Localhost / loopback are always included. |

## Routing and CSRF / CORS

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_EXTERNAL_HOST` | — | Host (with optional port) the HTTP redirector points at. **Required** unless `VANTYX_ALLOWED_HOSTS` is set. |
| `VANTYX_ALLOWED_HOSTS` | — | Comma-separated host allowlist for the HTTP redirector. Either this or `VANTYX_EXTERNAL_HOST` must be set. |
| `VANTYX_CORS_ALLOWED_ORIGINS` | — | Comma-separated origins to mirror in CORS headers. When empty, no CORS headers are emitted. |
| `VANTYX_DISABLE_ORIGIN_CHECK` | `0` | Set to `1` to skip the same-origin Origin/Referer check on cookie-authenticated mutations. Only do this for non-browser automation. |
| `VANTYX_TRUST_X_FORWARDED_FOR` | `0` | Set to `1` to honour `X-Forwarded-For` for the login rate limiter. |
| `VANTYX_WS_ALLOWED_ORIGINS` | — | Comma-separated origins allowed to open WebSockets. Defaults to the request's own origin. |
| `VANTYX_ALLOW_WS_NO_ORIGIN` | `0` | Set to `1` to permit WebSocket upgrades without an `Origin` header. Useful for local CLI tooling. |

## Authentication and rate limiting

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_LOGIN_RATE_LIMIT_N` | `5` | Maximum failed logins per IP within a 15-minute window before `429` is returned. |
| `VANTYX_TERMINAL_SESSION_IDLE_WARN_AFTER` | `30m` | Duration (Go duration syntax) before an idle terminal session is flagged. `0` disables idle warnings. |

## Storage

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_SQLITE_PATH` | `data/vantyx.db` | SQLite file path. Mount the parent directory to persist data across container restarts. |
| `VANTYX_RECORDINGS_DIR` | — | Directory where asciinema recordings are written. Recording metadata is only persisted when this is set. |
| `VANTYX_TFTP_ROOT` | `/app/data/tftp` | Root directory for the embedded TFTP server. Per-target subdirectories are created automatically. |
| `VANTYX_TFTP_LISTEN` | `0.0.0.0:6969` | UDP listen address for the embedded TFTP server. The container is non-root, so it cannot bind directly to port 69. |

## Access store performance tuning

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_ACCESS_QUERY_TIMEOUT` | — | SQL query timeout (Go duration syntax). |
| `VANTYX_ACCESS_DEFAULT_LIST_LIMIT` | — | Default page size for `ListTargets` and friends. |

## Cryptography

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY` | — | Base64-encoded 32-byte key used for AES-256-GCM encryption of stored SSH passwords. **Required** when operators may save SSH passwords on target records. Returns `503 Service Unavailable` if unset and a password write is attempted. |

## CLI gateway (`internal/sshd`)

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_SSH_LISTEN` | — | When set (for example `:2222`), the binary starts the CLI SSH gateway on this address. Leave unset to disable the gateway. |

## Audit forwarder

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_AUDIT_LOG_FILE` | — | Path to a persistent audit log file. When set, the audit sink also writes JSON lines here. |
| `VANTYX_AUDIT_FORWARD_PROTO` | — | `udp`, `tcp`, `unix`, or `unixgram`. When set together with `VANTYX_AUDIT_FORWARD_ADDR`, audit events are forwarded over syslog. |
| `VANTYX_AUDIT_FORWARD_ADDR` | — | Address for the forwarder (`host:port` or unix socket path). |
| `VANTYX_AUDIT_FORWARD_APP` | `vantyx` | Application tag attached to forwarded events. |
| `VANTYX_AUDIT_FORWARD_BUFFER` | `1024` | In-memory buffer size for the forwarder. |

## Telnet bridge

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_TELNET_WAKE_ON_CONNECT` | `1` | Set to `0` or `false` to suppress the wake-on-connect bytes Vantyx sends to dormant Telnet servers. |

## Environment files

For local development, `.env` is loaded by `docker compose`. Production
deployments should source these variables from your platform's secret
manager (Kubernetes Secret, AWS Secrets Manager, Doppler, etc.).
