# Roadmap

What is *not* yet in Vantyx and would be welcome contributions. Shipped
features are summarized in [README.md](../README.md#highlights) and the
topic guides under `docs/` (for example
[collaborative-sessions.md](collaborative-sessions.md),
[credentials.md](credentials.md)). This file only lists **open** work.

## Collaborative sessions (follow-ups)

Multi-user attach for SSH/Telnet, VNC, browser RDP, and CLI view-only
participation is documented in [collaborative-sessions.md](collaborative-sessions.md).
Remaining polish:

- **Direct RDP proxy** (`/ws/rdp`) multi-viewer — out of scope for browser RDP sharing.

## External authentication

OIDC SSO (Authorization Code + PKCE, any discovery-capable IdP) and TOTP
second factor for built-in users are shipped — see
[configuration.md](configuration.md#single-sign-on-openid-connect).
Remaining:

- **RADIUS / TACACS+ / LDAP** — external user directories.
- **WebAuthn / passkeys** as an alternative second factor.

## Storage and deployment

- **PostgreSQL backend** — Vantyx is SQLite-only today; the access /
  audit stores are designed to allow a Postgres driver.
- **Object storage for recordings and file transfers** — local disk only
  for now.
- **Kubernetes manifests / Helm chart** — single-container Docker image
  is the only first-class deployment artefact.

## Observability and auditing

- **Full shell history capture** — the command log records stdin lines
  but does not guarantee perfect reconstruction across complex TTY
  programs (vim, less, ncurses apps).
- **SIEM forwarder operational guide** — the wiring is in place
  (`internal/httpapi/audit_forwarder.go`), but operational documentation
  is still light.

## File transfer

- **Remote TFTP list / delete** — TFTP itself does not support
  enumeration, but a small inventory layer on top could help.

---

No commitment is implied about *when* any of these land. PRs welcome.
