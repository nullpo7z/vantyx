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
| Cookies | `HttpOnly`, `Secure` (under TLS), `SameSite=Strict`, `MaxAge=24h` | `internal/httpapi/auth_handler.go` |
| Rate limiting | 5 failures per IP per 15 min → `429`; `RemoteAddr` trusted by default, `X-Forwarded-For` opt-in via `VANTYX_TRUST_X_FORWARDED_FOR` | `internal/httpapi/login.go` |
| Access control | Per-user tag / group filtering; admin-only routes via `requireAdmin`; target-scoped routes via `getSessionAndTargetWithAccess` | `internal/access`, `internal/httpapi/auth_helpers.go` |
| Input validation | Whitelist regex + length for IDs / hostnames; protocol enum; parameterised SQL only | `internal/access/sqlite_store.go` |
| TLS | HTTPS mandatory, TLS 1.3 minimum, HTTP→HTTPS redirect, HSTS on HTTPS responses | `cmd/vantyx-server`, `internal/httpapi/middleware.go` |
| Cryptography at rest | Stored SSH passwords, private keys, and passphrases (targets, admin **Keys**, **Identities**) encrypted with AES-256-GCM via `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY`; list APIs expose presence flags only; plaintext zeroised after use | `internal/secret`, `internal/access/*_sqlite_store.go` |
| Error handling | 5xx responses use `writeInternalError` / `writeServiceUnavailableError` so internals never leak; details go to logs | `internal/httpapi/helpers.go` |
| CSRF / WS Origin | Same-origin Origin / Referer check on cookie-authenticated mutations; WebSocket `CheckOrigin` enforces same-origin (or `VANTYX_WS_ALLOWED_ORIGINS`) | `internal/httpapi/router.go`, `internal/httpapi/terminal.go` |
| Security headers | `Content-Security-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Permissions-Policy`, `Cross-Origin-Opener-Policy`, `Cross-Origin-Resource-Policy`, `Cross-Origin-Embedder-Policy: unsafe-none`, `Referrer-Policy`, `Cache-Control: no-store`; HSTS on HTTPS (`r.TLS != nil`) | `internal/httpapi/security_headers.go` (all routes via `NewRouter`) |
| Request limits | Global `MaxBytesReader` 2 MiB (non-multipart); per-file upload cap 64 MiB | `internal/httpapi/files.go` |
| Audit | Authentication events, terminal connect / deny, target CRUD logged via `internal/logging`; optional syslog / SIEM forwarder | `internal/logging`, `internal/httpapi/audit_forwarder.go` |
| Collaborative session invitations | Tokens are 32 random bytes from `crypto/rand`, returned exactly once at creation time; only the SHA-256 hash is persisted in `session_invitations`. TTL defaults to 15 min and is capped by `VANTYX_INVITATION_MAX_TTL_SECONDS`. The invitee must already log in to Vantyx; access to the underlying target is re-checked against the consuming user (not the inviter) at join time. Viewer stdin is dropped server-side regardless of any client-side toggle. Owner-only revoke clears the room and disconnects active viewers. | `internal/sharing`, `internal/httpapi/sharing.go` |
| SSH host key TOFU | Adopting / clearing the SSH host key fingerprint is admin-only and audited (`target_host_key_probed`, `target_host_key_adopted`, `target_host_key_cleared`). `POST /api/targets/probe-host-key` lets an admin dial any `host:port` once with `WithInsecureSkipHostKeyVerify` to capture a `SHA256:` fingerprint (no auth, no persistence). **A compromised admin session can use this to reach internal addresses** (SSRF-style); restrict admin access and network egress if that matters in your environment. Stored fingerprints are validated as `SHA256:<base64>` (no padding, OpenSSH default) before update. Mismatch on connect aborts the bridge and surfaces a structured `host_key_mismatch` WebSocket frame; the SPA forces a checkbox confirmation before adopting the new key. `VANTYX_SSH_INSECURE_IGNORE_HOST_KEY=1` disables verification on connect (emergency / lab only). | `internal/sshproxy/hostkey.go`, `internal/httpapi/targets_hostkey.go`, `internal/httpapi/terminal_hostkey_frame.go` |

## Known gaps and operational caveats

- **No session invalidation on password change** — sessions issued before a
  password update remain valid until their TTL expires. Implement if your
  policy requires it.
- **Encryption key rotation** — re-wrapping previously stored secrets (targets,
  Keys, Identities) is not implemented. Rotating `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY`
  makes existing records undecryptable.
- **Swagger UI CSP** — `/docs` allows `script-src` / `style-src` from
  `unpkg` for Swagger UI. The application surface itself does not.
- **Self-signed TLS** — the default auto-generated certificate is fine for
  evaluation but operators should mount their own certificate
  (`VANTYX_TLS_CERT_FILE` / `VANTYX_TLS_KEY_FILE`) for production.
- **`VANTYX_DISABLE_ORIGIN_CHECK=1`** — disables CSRF Origin/Referer checks on
  cookie-authenticated mutations. Non-browser automation only; never on a
  public deployment.
- **`VANTYX_ALLOW_PLAINTEXT_SECRETS=1`** — allows the server to start without
  `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY` and store SSH secrets without encryption.
  Tests only; never in production.
