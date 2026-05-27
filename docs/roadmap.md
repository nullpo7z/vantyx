# Roadmap

What is *not* yet in Vantyx and would be welcome contributions. The list
of shipped features lives in [README.md](../README.md#highlights) — this
file only enumerates open items so it does not double-track them.

## Remote desktop

- **RDP bridge.** Currently only VNC is supported; an RDP-to-VNC bridge
  (or FreeRDP wrapper) is planned.

## Collaborative sessions

Phase A (SSH/Telnet) shipped: invitation-based read-only viewer
attach, single-writer model with request/transfer of the write
token, named and link invitations, and an in-process bridge that
fans output out to every viewer. The remaining phases are open:

- **Phase B — VNC.** Multi-client viewer attach for the VNC proxy
  (likely via `x11vnc -shared` and per-client TCP fan-out in the
  WebSocket layer).
- **Phase C — RDP.** Once the VNC fan-out lands, the
  `xfreerdp + Xvfb + x11vnc` chain reuses it automatically.
- **Phase D — CLI viewer.** Add a "view-only attach" entry to the
  `internal/sshd` interactive menu so operators on the SSH CLI can
  participate as viewers too.
- **Mandatory recording when viewers are present.** Optional
  `VANTYX_REQUIRE_RECORDING_WITH_VIEWERS` flag to refuse new
  invitations on sessions that are not being recorded.

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
