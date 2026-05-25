# Vantyx roadmap and feature matrix

[日本語](roadmap.ja.md)

This is a snapshot of what Vantyx supports today and what is on the
roadmap. It mirrors the implementation, not aspirational design notes.

---

## Shipped

### Platform and project structure

- Go backend + Tailwind-based SPA in a single repository.
- Environment-variable + `.env` configuration loading.
- SQLite persistence with migrations.
- Single-container Docker / docker-compose deployment (backend + frontend).
- Health endpoint at `/healthz`.
- Mandatory HTTPS with HTTP → HTTPS redirect and automatic self-signed
  certificate generation.

### Authentication and authorisation

- Local user management (create, list, change password).
- Password authentication with bcrypt hashing and a complexity policy
  (≥8 chars, upper, lower, digit, symbol).
- Cookie-based server-side sessions (24 h TTL).
- Login / logout flows and forced initial password change.
- Login rate limiting (5 failures per IP per 15 min → `429`).
- Role split (admin / user) gating `/api/spec` and `/docs`.
- Many-to-many access groups, users, and targets connected by tags.

### SSH / Telnet, session persistence, and CLI gateway

- Browser terminal (xterm.js + WebSocket `/ws/ssh`) for SSH and Telnet
  targets.
- Proxies: `internal/sshproxy`, `internal/telnetproxy`. Session manager:
  `internal/session`.
- Telnet: NAWS for terminal resize, stored-credential auto-login.
- Friendly diagnostic messages on dial / connection failure.
- Session persistence and resume — closing the browser tab keeps the
  session running in the background; explicit termination via the list
  UI or `DELETE /api/terminal/sessions/{id}`.
- CLI session controls: `Ctrl+]` detaches to the menu, `Ctrl+D` during
  `connect` terminates the session.
- Active session list (nav "Sessions") filtered server-side to the
  logged-in user’s accessible targets; SSE updates for SSH / Telnet /
  RDP, plus re-connect and terminate actions.
- Idle warning system driven by
  `VANTYX_TERMINAL_SESSION_IDLE_WARN_AFTER` (default 30 min, `0` to
  disable). Sessions are *not* auto-terminated.
- CLI gateway: Vantyx itself listens for SSH (`internal/sshd`), letting
  operators `ssh user@vantyx` and reach any target they can access.
- SSH public-key registration (`/api/me/ssh-keys`) for CLI gateway login.
- Target protocols: SSH, Telnet, VNC (selectable on create / edit).

### Recordings and audit trail

- asciinema-format terminal recording (`internal/recording`).
- Recording metadata persisted to the DB when `VANTYX_RECORDINGS_DIR` is
  set.
- Recording list (`GET /api/recordings`) and download
  (`GET /api/recordings/{id}/file`).
- SPA: recording list + asciinema-player playback.
- Audit log search by time range / user / target; command log search
  with `after_id` / `next_cursor` keyset pagination.

### File transfer

- SFTP back-end (`internal/sftp`): list, upload, download, delete.
- FTP back-end (`internal/ftp`): same API for FTP targets.
- TFTP client: register a `protocol=tftp` target and use the "Files"
  link on the home page to Get / Put against a remote TFTP server.
- TFTP server (embedded): enabled on SSH / Telnet targets via the home
  toggle. The auto-created TFTP target is hidden from the list.
  REST surface: `/api/tftp/targets/{id}/files/*`.
- File manager UI (`/files`): directory tree, list, transfer, and
  delete for SFTP / FTP; path-based transfer for TFTP.
- Background file transfer: `GET/POST/DELETE /api/file-transfers/*`,
  with progress visible from the global footer bar and the sessions
  list. The `running` phase continues after the page is left.
- Limits: jobs live in memory only (cleared on restart); aborting
  during the `receiving` phase fails the transfer; single-file cap is
  64 MiB.
- TFTP at startup: the auto-created TFTP rows are deleted, external
  TFTP targets are preserved, and the embedded server resumes
  automatically if any TFTP target remains.

### VNC remote desktop

- VNC WebSocket proxy (`internal/vncproxy`) bridging browser ↔ VNC
  server bytes.
- `GET /ws/vnc?target_id=...` paired with the noVNC client.
- SPA viewer at `/vnc` powered by noVNC (loaded from CDN).

### Targets, groups, and tags

- CRUD for targets, group binding, stored SSH credentials (password and
  private key).
- SSH passwords are encrypted at rest with AES-256-GCM keyed by
  `VANTYX_SSH_PASSWORD_ENCRYPTION_KEY`.
- Tagging for groups, users, and targets with tag-based access control.
- Group membership management (add / remove).

### Security and OWASP ASVS L2

- Designed against OWASP ASVS Level 2; see
  [SECURITY-ASVS-L2.md](SECURITY-ASVS-L2.md).
- Cookies use `HttpOnly`, `Secure` (under TLS), `SameSite=Lax`, and
  rotate on login (fixation protection).
- Input validation (IDs, hostnames, protocol whitelist), parameterised
  SQL, output escaping.
- Audit log persistence in `audit_logs` with optional syslog / SIEM
  forwarder configured via `/api/settings/audit-forwarder`.

### API and frontend

- REST API for login, logout, me, password change, groups, users,
  targets, tags, recordings, file operations.
- Swagger-style API spec (`/api/spec`) and admin docs UI (`/docs`).
- SPA covers login, dashboard, group tree, target list / edit, active
  session list, and the terminal / VNC / files / recordings flows.

---

## Roadmap

### Remote desktop

- **RDP bridge** — currently only VNC is supported; an RDP-to-VNC
  bridge or FreeRDP wrapper is planned.

### External authentication

- **OIDC SSO** — integration with external IdPs (Keycloak / Azure AD /
  Okta) is not yet implemented.
- **TOTP MFA** — TOTP-based second factor.
- **RADIUS / TACACS+ / LDAP** — external user directories.

### Operations and infrastructure

- **PostgreSQL** — only SQLite today; the original design contemplates a
  PostgreSQL deployment option.
- **Object storage** — recording / file storage is local-only; cloud
  storage adapters are not implemented.
- **Kubernetes manifests** — `deploy/k8s` or a Helm chart is not yet
  provided.

### Tracing and observability

- **Full shell history capture** — the command log records stdin lines
  but does not guarantee perfect reconstruction across complex TTY
  programs.
- **SIEM forwarder operational hardening** — the wiring is in place
  but operational guidance is still light.

---

## Summary

| Area | Today | Roadmap |
|------|-------|---------|
| Auth & session | Local auth, cookie sessions, rate limit | OIDC, TOTP, RADIUS / TACACS+ / LDAP |
| Access control | Groups, targets, tags, horizontal / vertical | — |
| Terminal | SSH / Telnet via browser + CLI, persistence, resume, Telnet NAWS / auto-login | — |
| Recording & audit | asciinema recording, search by time / user / target, audit + command logs with pagination | Full shell history capture |
| File transfer | SFTP / FTP, TFTP, file UI, background transfers, sessions UI | DB-persisted transfer jobs; remote TFTP list / delete (protocol bound) |
| Remote desktop | VNC (noVNC + WS proxy) | RDP bridge |
| Security | ASVS L2, encryption, audit log forwarder | — |
| Infrastructure | Docker single container, SQLite, self-signed TLS | PostgreSQL, object storage, Kubernetes |
