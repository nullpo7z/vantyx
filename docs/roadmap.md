# Roadmap

What is *not* yet in Vantyx and would be welcome contributions. The list
of shipped features lives in [README.md](../README.md#highlights) — this
file only enumerates open items so it does not double-track them.

## Remote desktop

- **RDP bridge.** Currently only VNC is supported; an RDP-to-VNC bridge
  (or FreeRDP wrapper) is planned.

## External authentication

- **OIDC SSO** — integration with external IdPs (Keycloak / Azure AD /
  Okta).
- **TOTP MFA** — second factor for the built-in users.
- **RADIUS / TACACS+ / LDAP** — external user directories.

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

- **DB-persisted transfer jobs** — currently jobs live in memory and are
  lost on restart.
- **Remote TFTP list / delete** — TFTP itself does not support
  enumeration, but a small inventory layer on top could help.

---

No commitment is implied about *when* any of these land. PRs welcome.
