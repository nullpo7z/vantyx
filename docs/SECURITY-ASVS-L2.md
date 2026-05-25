# OWASP ASVS Level 2 — security baseline

[日本語](SECURITY-ASVS-L2.ja.md)

This document is a self-assessment of Vantyx against the
[OWASP Application Security Verification Standard](https://owasp.org/www-project-application-security-verification-standard/)
(ASVS) Level 2. It also records the deployment recommendations we follow.

## 0. Priority gaps and current status

- **P0 (critical)**
  - **WebSocket Origin validation (CSWSH)** — resolved (same-origin /
    allowlist enforced).
  - **RDP browser credentials in URL query** — resolved (no longer
    accepted via query string).
  - **Secrets stored in `localStorage`** — resolved (replaced by
    `BroadcastChannel`).
- **P1 (high)**
  - **CSRF on cookie-authenticated mutations** — resolved (Origin /
    Referer check for browser clients).
  - **Global request body limit** — resolved (2 MiB, excluding multipart).
  - **`X-Forwarded-For` trust boundary** — resolved (off by default,
    opt-in via env).
  - **`target=_blank` opener abuse** — resolved (VNC/RDP use
    `noopener`; terminal uses `parent_token` + `noopener`).
- **P2 (medium)**
  - **CSP scoping for `/docs`**, **key rotation**, and **invalidate
    sessions on password change** are deferred and depend on operational
    requirements (see the recommendations section at the end).

## 1. Authentication

| Requirement | Status | Notes |
|-------------|--------|-------|
| V2.1 Use password or strong alternative authentication | OK | bcrypt-hashed passwords (`internal/auth/password.go`). |
| V2.2 Forbid common / default credentials | OK | Default admin password `Admin123!` (policy-compliant). When it is unchanged the login response returns `require_password_change` to force rotation. |
| V2.3 Password complexity policy | OK | `auth.ValidatePassword` enforces 8+ chars, upper, lower, digit, symbol on creation and update. |
| V2.4 Invalidate sessions on logout | OK | `POST /api/logout` removes the server-side session and clears the cookie. |
| V2.5 Account lockout / brute force | OK | `LoginRateLimiter` returns `429 Too Many Requests` after 5 failures per IP within 15 minutes. |
| V2.6 Generic error on authentication failure | OK | `invalid credentials` is returned to avoid user enumeration. |

## 2. Session management

| Requirement | Status | Notes |
|-------------|--------|-------|
| V3.1 Cryptographic randomness of session IDs | OK | 32 bytes from `crypto/rand`, hex-encoded (`auth/session.go`). |
| V3.2 Session invalidation (logout / timeout) | OK | Logout deletes the session; reads check 24 h TTL. |
| V3.3 Cookie `HttpOnly` / `Secure` / `SameSite` | OK | `HttpOnly`, `SameSite=Lax`, `Secure` when TLS is in use, `MaxAge=24h`. |
| V3.4 Session fixation protection | OK | A new session ID is issued on successful login. |
| V3.x WebSocket session protection | OK | Cookie authentication plus WebSocket Origin validation (`internal/httpapi/terminal.go`) mitigates CSWSH. |

## 3. Access control

| Requirement | Status | Notes |
|-------------|--------|-------|
| V4.1 Authentication required for sensitive APIs | OK | Each handler checks the session cookie; unauthenticated requests get 401. |
| V4.2 Horizontal and vertical access control | OK | Targets and groups are filtered per user via `TargetIDsForUser`; cross-user `target_id` requests return 403. |
| V4.3 Restrict administrative functions | OK | `/api/spec` and `/docs` are gated by `requireAdmin`. |
| V4.4 Server-side authorisation | OK | Target lists, terminal sessions, file transfers, etc. all enforce authorisation server-side. |

## 4. Input validation and encoding

| Requirement | Status | Notes |
|-------------|--------|-------|
| V5.1 Whitelist input validation | OK | Group / target IDs are constrained by regex and length limits (`access/sqlite_store.go`); protocols are limited to a fixed list. |
| V5.2 SQL injection | OK | All SQL goes through `?` placeholders (`ExecContext` / `QueryRowContext`); no string concatenation. |
| V5.3 Output encoding (XSS) | OK | The SPA escapes user data via `escapeHtml()` before rendering. |
| V5.4 Dangerous path handling | OK | Hostnames are IP or hostname patterns; `filepath.Clean` mitigates traversal. |

## 5. Cryptography

| Requirement | Status | Notes |
|-------------|--------|-------|
| V7.1 Encrypted transport for sensitive data | OK | HTTPS required; TLS 1.3 minimum; HTTP redirects to HTTPS. |
| V7.2 Strong algorithms | OK | bcrypt (passwords), AES-256-GCM (stored SSH passwords), TLS 1.3. |
| V7.3 Secret / key management | OK | The encryption key comes from `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY` (32-byte Base64). Plaintext buffers are zeroised after use. |
| V7.4 Quality of randomness | OK | `crypto/rand` used for session IDs, nonces, and self-signed certificate serials. |

## 6. Error handling and logging

| Requirement | Status | Notes |
|-------------|--------|-------|
| V8.1 No stack traces / internals returned to clients | OK | 5xx responses use `writeInternalError` / `writeServiceUnavailableError` with generic messages; details are logged server-side. |
| V8.2 Audit logging of authentication events | OK | Login successes / failures and terminal connections / denials are emitted through the `internal/logging` audit pipeline. |

## 7. Data protection

| Requirement | Status | Notes |
|-------------|--------|-------|
| V9.1 Sensitive data protected at rest | OK | SSH passwords are encrypted with AES-256-GCM (`internal/secret`); the key is read from the environment. |
| V9.2 Minimise stored sensitive data | OK | Sessions store only the user ID and expiry; passwords are never stored, only their bcrypt hashes. |

## 8. Communications

| Requirement | Status | Notes |
|-------------|--------|-------|
| V10.1 TLS in production | OK | HTTPS is mandatory; TLS 1.3. |
| V10.2 Certificate validation | Partial | Browsers validate the server certificate. Operators of self-signed deployments must add a trust exception or distribute their own CA. |
| V10.3 Safe redirects | OK | Redirect targets are constrained to `VANTYX_EXTERNAL_HOST` or `VANTYX_ALLOWED_HOSTS`. |

## 9. Security headers and configuration

| Requirement | Status | Notes |
|-------------|--------|-------|
| V14.4.1 X-Frame-Options | OK | `DENY`. |
| V14.4.2 CORS | OK | Only origins listed in `VANTYX_CORS_ALLOWED_ORIGINS` are accepted. |
| V14.4.3 Content-Security-Policy | OK | `default-src 'self'`; `wasm-unsafe-eval` is allowed for the asciinema player; `script-src` / `style-src` allow `self` and `unpkg` (Swagger UI); `frame-ancestors 'none'`. |
| V14.4.4 X-Content-Type-Options | OK | `nosniff`. |
| V14.4.6 HSTS | OK | `Strict-Transport-Security` is sent on HTTPS responses only. |
| V14.4 Cache-Control | OK | `no-store, max-age=0`. |

## 10. API and web services

| Requirement | Status | Notes |
|-------------|--------|-------|
| Authentication consistency | OK | REST and WebSocket (`/ws/ssh`, `/ws/vnc`, `/ws/rdp`) all require the session cookie. |
| Authorisation consistency | OK | Targets, groups, and terminals all verify user permissions on the server. |
| Mass-assignment hardening | OK | Create / update requests only accept defined fields (e.g. `createGroupRequest`). |
| CSRF on cookie-authenticated mutations | OK | Mutating requests require same-origin `Origin` or `Referer` (`internal/httpapi/router.go`). |
| Request-size limits | OK | Global `MaxBytesReader` of 2 MiB plus an upload-specific 64 MiB cap (`internal/httpapi/files.go`). |

## Improvements made as part of this assessment

1. **Cookie `Secure` / `MaxAge`** — login sets `Secure` (when TLS is
   active) and `MaxAge: 24*3600` so the browser cookie matches the
   server-side TTL.
2. **Server-side logout** — `POST /api/logout` removes the session from
   the store and clears the cookie; the SPA invokes it on log out.
3. **Login rate limiting (ASVS V2.5)** — `App.LoginRateLimiter`
   returns `429 Too Many Requests` after 5 failures per IP within
   15 minutes.
4. **Password policy (ASVS V2.3)** — `auth.ValidatePassword` enforces
   ≥8 chars, upper, lower, digit, and symbol on user creation and on
   password update. The default admin password is `Admin123!`.
5. **Forced password change (ASVS V2.2)** — the login response returns
   `require_password_change: true` when the admin still uses the default
   password. The SPA shows a change-password modal that calls
   `POST /api/me/password` (which goes through `UserStore.UpdatePassword`).
6. **No internal error leakage (V8.1)** — `writeInternalError` and
   `writeServiceUnavailableError` are used wherever the server might
   surface `err.Error()`. Clients only see `internal error` /
   `service unavailable`; details are logged server-side.
7. **WebSocket Origin enforcement (CSWSH)** —
   `websocket.Upgrader.CheckOrigin` enforces same-origin (or
   `VANTYX_WS_ALLOWED_ORIGINS`) and rejects missing `Origin` by default
   (override with `VANTYX_ALLOW_WS_NO_ORIGIN=1` for local-only setups).
8. **RDP browser credentials no longer accepted via URL query** —
   `/ws/rdp/browser` ignores `rdp_user` / `rdp_pass` in the query string,
   removing them from history / proxy logs. Only stored credentials are
   used.
9. **Drop `localStorage` for secrets** — terminal credential prompts
   (password / passphrase) are exchanged through `BroadcastChannel`
   instead of `localStorage`.
10. **CSRF mitigation for cookie-authenticated mutations** — POST / PUT
    / PATCH / DELETE with the session cookie must include a same-origin
    `Origin` or `Referer`. Disable via `VANTYX_DISABLE_ORIGIN_CHECK=1`
    only for automated, non-browser callers.
11. **Global request body limit (DoS)** — `MaxBytesReader` (2 MiB) is
    applied to non-multipart requests; uploads keep their 64 MiB limit.
12. **`X-Forwarded-For` trust boundary** — the rate limiter trusts
    `RemoteAddr` by default and uses `X-Forwarded-For` only when
    `VANTYX_TRUST_X_FORWARDED_FOR=1`.
13. **`target=_blank` hardening** — VNC / RDP new tabs add
    `rel="noopener noreferrer"`. Terminal links use `window.open(...,
    'noopener')` and rely on `parent_token` (via `BroadcastChannel`) to
    let the child tab return data to the parent.

## Recommended deployment hardening

- **Persistent audit log** — `internal/logging` writes audit events to
  the DB and a log file. For production we recommend also forwarding to
  an external syslog / SIEM endpoint
  (`VANTYX_AUDIT_FORWARDER_*` configures the built-in forwarder).
- **Invalidate sessions on password change** — implement if your policy
  requires it (Vantyx does not do this today).
- **Tighten CSP for `/docs`** — split the policy for Swagger UI from the
  application so the latter can drop `unsafe-inline` etc.
- **Encryption key rotation** — design a multi-generation / re-wrap
  workflow if `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY` rotation is required.
- **Host header check (HTTPS)** — depending on deployment topology you
  may want to extend the HTTPS listener with the same host allowlist
  the HTTP redirector uses.

---

*Last reviewed: 2026. Verified against ASVS 4.0 Level 2 with the
implementation in this repository.*
