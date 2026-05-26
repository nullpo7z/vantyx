# Security notes (best-effort)

Vantyx is a small personal OSS project, but it terminates remote access to
SSH / RDP / VNC / Telnet servers, so it tries to follow the spirit of
[OWASP ASVS](https://owasp.org/www-project-application-security-verification-standard/)
Level 2 for sensitive data at rest and in transit. **This is a best-effort
self-checklist, not a formally audited statement.** Treat it as a quick
overview and read the code for the source of truth.

## Implemented controls

| Area | What we do | Where |
|------|------------|-------|
| Password auth | bcrypt hashing, complexity policy (≥8 chars, upper / lower / digit / symbol), forced rotation of the default admin password | `internal/auth/password.go` |
| Sessions | 32-byte random IDs from `crypto/rand`, server-side store, 24 h TTL, regenerated on login, invalidated on logout | `internal/auth/session.go` |
| Cookies | `HttpOnly`, `Secure` (under TLS), `SameSite=Lax`, `MaxAge=24h` | `internal/httpapi/auth_handler.go` |
| Rate limiting | 5 failures per IP per 15 min → `429`; `RemoteAddr` trusted by default, `X-Forwarded-For` opt-in via `VANTYX_TRUST_X_FORWARDED_FOR` | `internal/httpapi/login.go` |
| Access control | Per-user tag / group filtering; admin-only routes via `requireAdmin`; target-scoped routes via `getSessionAndTargetWithAccess` | `internal/access`, `internal/httpapi/auth_helpers.go` |
| Input validation | Whitelist regex + length for IDs / hostnames; protocol enum; parameterised SQL only | `internal/access/sqlite_store.go` |
| TLS | HTTPS mandatory, TLS 1.3 minimum, HTTP→HTTPS redirect, HSTS on HTTPS responses | `cmd/vantyx-server`, `internal/httpapi/middleware.go` |
| Cryptography at rest | Stored SSH passwords encrypted with AES-256-GCM, key from `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY`; plaintext zeroised after use | `internal/secret` |
| Error handling | 5xx responses use `writeInternalError` / `writeServiceUnavailableError` so internals never leak; details go to logs | `internal/httpapi/helpers.go` |
| CSRF / WS Origin | Same-origin Origin / Referer check on cookie-authenticated mutations; WebSocket `CheckOrigin` enforces same-origin (or `VANTYX_WS_ALLOWED_ORIGINS`) | `internal/httpapi/router.go`, `internal/httpapi/terminal.go` |
| Security headers | `Content-Security-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Cache-Control: no-store` | `internal/httpapi/middleware.go` |
| Request limits | Global `MaxBytesReader` 2 MiB (non-multipart); per-file upload cap 64 MiB | `internal/httpapi/files.go` |
| Audit | Authentication events, terminal connect / deny, target CRUD logged via `internal/logging`; optional syslog / SIEM forwarder | `internal/logging`, `internal/httpapi/audit_forwarder.go` |

## Known gaps and operational caveats

- **No session invalidation on password change** — sessions issued before a
  password update remain valid until their TTL expires. Implement if your
  policy requires it.
- **Encryption key rotation** — re-wrapping previously stored SSH passwords
  is not implemented. Rotating `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY` makes
  existing records undecryptable.
- **Swagger UI CSP** — `/docs` allows `script-src` / `style-src` from
  `unpkg` for Swagger UI. The application surface itself does not.
- **Self-signed TLS** — the default auto-generated certificate is fine for
  evaluation but operators should mount their own certificate
  (`VANTYX_TLS_CERT_FILE` / `VANTYX_TLS_KEY_FILE`) for production.
- **`VANTYX_DISABLE_ORIGIN_CHECK=1`** — exists for non-browser automation
  only. Never enable this on a publicly exposed deployment.
