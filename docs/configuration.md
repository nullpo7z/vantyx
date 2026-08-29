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
| `VANTYX_DISABLE_ORIGIN_CHECK` | `0` | Set to `1` to skip the same-origin Origin/Referer check on cookie-authenticated mutations. **Never enable on production.** Automation / tests only. |
| `VANTYX_DISABLE_SECURITY_HEADERS` | `0` | Set to `1` to omit CSP / X-Frame-Options / HSTS and related headers. **Tests only.** |
| `VANTYX_TRUST_X_FORWARDED_FOR` | `0` | Set to `1` to honour `X-Forwarded-For` for the login rate limiter. |
| `VANTYX_WS_ALLOWED_ORIGINS` | — | Comma-separated origins allowed to open WebSockets. Defaults to the request's own origin. |
| `VANTYX_ALLOW_WS_NO_ORIGIN` | `0` | Set to `1` to permit WebSocket upgrades without an `Origin` header. Useful for local CLI tooling. |

## Authentication and rate limiting

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_LOGIN_RATE_LIMIT_N` | `5` | Maximum failed logins per IP within a 15-minute window before `429` is returned. |
| `VANTYX_TERMINAL_SESSION_IDLE_WARN_AFTER` | `30m` | Duration (Go duration syntax) before an idle terminal session is flagged. `0` disables idle warnings. |
| `VANTYX_INVITATION_MAX_TTL_SECONDS` | `14400` | Maximum validity (`ttl_seconds`) for collaborative session invitations. Default TTL when omitted is 15 minutes. See [collaborative-sessions.md](collaborative-sessions.md). |
| `VANTYX_REQUIRE_RECORDING_WITH_VIEWERS` | unset | When enabled (`1`, `true`, or `yes`), refuse collaborative invitations and joins unless `VANTYX_RECORDINGS_DIR` is set. |

### Two-factor authentication (TOTP)

TOTP needs no configuration: every user can enable it from **Account →
Two-factor authentication** in the web UI (RFC 6238, SHA-1 / 30 s / 6 digits,
compatible with Google Authenticator, Authy, 1Password, …). Enabling it issues
eight single-use recovery codes that are shown once.

- **Web UI**: `POST /api/login` answers `{"mfa_required":true,"mfa_token":…}`
  instead of a session; the client completes with
  `POST /api/login/totp {mfa_token, code}`. The token lives 5 minutes and is
  discarded after 5 wrong codes (the login rate limiter applies as well).
- **CLI gateway**: not affected — the gateway accepts **public keys only**
  (no password or keyboard-interactive auth), so there is no password
  entry point for TOTP to protect. Register keys under **Users → Public
  keys**.
- **SSO logins** (below) are not challenged for a local TOTP — the IdP owns
  MFA for those users.
- Users disable it with their password (`DELETE /api/me/totp`); admins can
  clear a locked-out user's factor with `DELETE /api/users/{id}/totp`
  (**Users → Reset 2FA**).
- Secrets are stored AES-256-GCM encrypted with
  `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY`; recovery codes are stored as SHA-256
  digests.

## Single sign-on (OpenID Connect)

Vantyx can act as an OIDC relying party (Authorization Code flow with PKCE
S256 and nonce). SSO is enabled when both `VANTYX_OIDC_ISSUER` and
`VANTYX_OIDC_CLIENT_ID` are set; the login page then shows a
"Sign in with …" button. Password login stays available.

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_OIDC_ISSUER` | — | Issuer URL used for discovery (`<issuer>/.well-known/openid-configuration`), e.g. `https://keycloak.example/realms/ops`, `https://login.microsoftonline.com/<tenant>/v2.0`, `https://<org>.okta.com`. Discovery happens lazily on the first login, so an unreachable IdP does not block start-up. |
| `VANTYX_OIDC_DISCOVERY_URL` | = issuer | Base URL whose `/.well-known/openid-configuration` is fetched when it differs from the `iss` the IdP puts in ID tokens. Cloudflare Access SaaS apps need this: `VANTYX_OIDC_ISSUER=https://<team>.cloudflareaccess.com`, `VANTYX_OIDC_DISCOVERY_URL=https://<team>.cloudflareaccess.com/cdn-cgi/access/sso/oidc/<client-id>`. Tokens are still verified against `VANTYX_OIDC_ISSUER`. |
| `VANTYX_OIDC_CLIENT_ID` | — | Client ID registered at the IdP. |
| `VANTYX_OIDC_CLIENT_SECRET` | — | Client secret. Optional for public clients (PKCE is always used). |
| `VANTYX_OIDC_REDIRECT_URL` | derived | Callback URL registered at the IdP. Defaults to `<scheme>://<host>/api/auth/oidc/callback` from the incoming request (honours `X-Forwarded-Proto`); set it explicitly behind a reverse proxy. |
| `VANTYX_OIDC_SCOPES` | `openid profile email` | Space- or comma-separated scopes. `openid` is always added. |
| `VANTYX_OIDC_USERNAME_CLAIM` | `preferred_username` | ID-token claim mapped to the Vantyx username. Falls back to `preferred_username`, then `email`. |
| `VANTYX_OIDC_AUTO_CREATE_USERS` | `0` | `1`/`true`: create unknown users as role `user` with an unusable random password. Otherwise unknown identities are rejected with `not_provisioned`. |
| `VANTYX_OIDC_DISPLAY_NAME` | `SSO` | Label for the login button ("Sign in with *name*"). |

Identity mapping on each login, in order: an existing link (`issuer`,
`sub`) → a local user whose username equals the username claim
(case-insensitive; the link is stored on first use) → auto-create when
enabled. Links survive username changes at the IdP. Deleting the local user
removes its links. Deep links (`/?next=/terminal?…`) are carried through the
IdP round trip; only same-origin paths are honoured.

Audit events: `oidc_login_started`, `oidc_login_ok`, `oidc_login_failed`
(with `reason`), `oidc_user_created`.

### Example: Cloudflare Access (Zero Trust) as the IdP

1. Zero Trust dashboard → **Access → Applications → Add an application →
   SaaS**, protocol **OIDC**. Redirect URL:
   `https://<vantyx-host>/api/auth/oidc/callback`. Enable **PKCE**. Keep the
   default scopes (`openid email profile`). Assign an Access policy (who may
   log in) and save; note the *Client ID*, *Client secret* and the
   endpoints shown.
2. Cloudflare ID tokens carry `sub`, `email`, `name` (no
   `preferred_username`), so map usernames to the e-mail address:

   ```
   VANTYX_OIDC_ISSUER=https://<team>.cloudflareaccess.com
   VANTYX_OIDC_DISCOVERY_URL=https://<team>.cloudflareaccess.com/cdn-cgi/access/sso/oidc/<client-id>
   VANTYX_OIDC_CLIENT_ID=<client-id>
   VANTYX_OIDC_CLIENT_SECRET=<client-secret>
   VANTYX_OIDC_REDIRECT_URL=https://<vantyx-host>/api/auth/oidc/callback
   VANTYX_OIDC_USERNAME_CLAIM=email
   VANTYX_OIDC_AUTO_CREATE_USERS=1
   VANTYX_OIDC_DISPLAY_NAME=Cloudflare
   ```

   If the discovery document's `issuer` turns out to equal the
   `/cdn-cgi/access/sso/oidc/<client-id>` base URL, set `VANTYX_OIDC_ISSUER`
   to that value instead and drop `VANTYX_OIDC_DISCOVERY_URL`; the
   `oidc_login_failed` audit event (`reason=id_token_invalid`, "issuer did
   not match") tells you which one applies.
3. Without auto-create, pre-create the user with the e-mail address as
   the username; Vantyx links the Cloudflare identity to it on first login.

## Time and timezone

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_TIMEZONE` | `UTC` | IANA zone name (e.g. `Asia/Tokyo`) used for **every human-facing timestamp**: container logs, the audit log file and syslog forward, the web UI and the CLI gateway all show the same clock, so an entry on screen can be matched to a log line without conversion. Timestamps are written with an explicit offset (`2026-08-30T00:07:47+09:00`); database storage stays UTC. There is deliberately no per-user or UI override. An unknown name aborts start-up. |

## Storage

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_SQLITE_PATH` | `data/vantyx.db` | SQLite file path. Mount the parent directory to persist data across container restarts. |
| `VANTYX_RECORDINGS_DIR` | — | Directory for session recordings: asciinema `.cast` (terminal/CLI) and H.264 `.mp4` (RDP/VNC screen capture). Metadata is only persisted when this is set. GIF/MP4 export temp files are stored under `{data_dir}/recording-exports`, not here. |
| `VANTYX_TFTP_ROOT` | `/app/data/tftp` | Root directory for the embedded TFTP server. Per-target subdirectories are created automatically. |
| `VANTYX_TFTP_LISTEN` | `0.0.0.0:6969` | UDP listen address for the embedded TFTP server. The container is non-root, so it cannot bind directly to port 69. |

## Recording exports

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_RECORDING_EXPORT_CONVERT_TIMEOUT` | `45m` | Maximum wall time for a single GIF/MP4 export conversion job. |
| `VANTYX_RECORDING_EXPORT_COMPLETED_TTL` | `168h` | How long completed, failed, or cancelled export jobs (and their output files) are retained in memory and on disk before automatic cleanup. |

## Access store performance tuning

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_ACCESS_QUERY_TIMEOUT` | — | SQL query timeout (Go duration syntax). |
| `VANTYX_ACCESS_DEFAULT_LIST_LIMIT` | — | Default page size for `ListTargets` and friends. |

## Cryptography

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY` | — | Base64-encoded 32-byte key used for AES-256-GCM encryption of sensitive SSH material at rest: target passwords/keys, **Keys** (`ssh_keys`), and **Identities** (`credential_identities`). **Required** when any of these may be stored. Returns `503 Service Unavailable` if unset and a write that needs encryption is attempted. See [credentials.md](credentials.md). |
| `VANTYX_INITIAL_ADMIN_PASSWORD` | — | Password for the bootstrap `admin` user on first start. When unset, a random password is printed once to stdout/logs. |
| `VANTYX_ALLOW_PLAINTEXT_SECRETS` | `0` | Set to `1` to allow starting without `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY` (secrets stored unencrypted). **Never enable on production.** |

## CLI gateway (`internal/sshd`)

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_SSH_LISTEN` | — | When set (for example `:2222`), the binary starts the CLI SSH gateway on this address. Leave unset to disable the gateway. Authentication is **public-key only**: password and keyboard-interactive auth are not offered, so a leaked password cannot bypass the web UI's second factor. Admins register keys per user under **Users → Public keys** (`POST /api/users/{id}/ssh-keys`). |

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

For Docker Compose, copy [`.env.example`](../.env.example) to `.env` before
starting the stack:

- **Operations:** [docker-compose.yml](../docker-compose.yml) — minimal pull-only
  stack; required `VANTYX_*` variables live in `.env` (see [`.env.example`](../.env.example)).
- **Development:** [docker-compose.dev.yml](../docker-compose.dev.yml) — builds
  from the repository Dockerfile.

Production deployments should source these variables from your platform's
secret manager (Kubernetes Secret, AWS Secrets Manager, Doppler, etc.).
