# Collaborative sessions

Vantyx lets multiple logged-in users attach to the **same** remote session.
One participant holds the **write token** (input is forwarded to the remote
host); everyone else is a **viewer** whose input is dropped on the server.

Supported today:

| Channel | Protocol | Browser | CLI (`sshd`) |
|---------|----------|---------|--------------|
| Terminal | SSH / Telnet | Yes | View-only via `join` / `watch` |
| VNC | Direct VNC targets | Yes | — |
| RDP | Browser (FreeRDP → Xvfb → x11vnc) | Yes | — |

## Concepts

| Term | Meaning |
|------|---------|
| **Owner** | User who started the session. Can invite, kick, and revoke. |
| **Writer** | Participant with the write token (usually the owner until they transfer control). |
| **Viewer** | Read-only attach; sees live output but cannot send input unless granted the write token. |
| **Invitation** | Time-limited grant to join a session. Plain token is shown **once** at creation; only a SHA-256 hash is stored. |
| **Named invitation** | Targets a specific Vantyx user (`invitee_user_id`). Appears in their incoming-invitations list. |
| **Link invitation** | Shareable URL/token; any logged-in user who satisfies target access can join (subject to `max_uses`). |
| **Group invitation** | One named invite per member of an access group that contains the target; the owner must belong to that group. |

Access to the **underlying target** is always re-checked for the **joining**
user at join time, and the **inviter’s** target access is re-checked as well
(stale-access guard). A viewer without target permission cannot join even
with a valid token.

## User workflow (browser)

1. User A opens a session to a target (writer mode).
2. From the session UI, A creates a **named**, **tag**, **group**, or **link** invitation (optional TTL, optional use limit for links).
3. User B logs in, accepts the invitation (incoming list or invite link), and calls **`POST .../join`** with the token.
4. B attaches as **viewer** via WebSocket (`mode=viewer`).
5. B may **request control** (terminal only today); A (current writer) can **grant** or **deny**.
6. A can **release** the write token or **kick** a viewer.
7. Revoking an invitation disconnects active viewers who joined via that invite.

The UI stays in sync via **SSE** (`GET /api/events/sessions`).

## CLI workflow (`sshd`)

1. Accept a link invitation token: `join <token>` (or `join` and enter the token when prompted).
2. Attach view-only: `watch <n>` from the numbered list of sessions you have joined.
3. Input is disabled in view-only mode; press **Ctrl+]** to return to the menu.

CLI `connect` sessions register with the sharing bridge so web viewers and write-token handoff work consistently.

## WebSocket attach

### SSH / Telnet

```http
GET /ws/ssh?session_id=<id>&mode=writer|viewer
```

### VNC

```http
GET /ws/vnc?target_id=<id>                    # new session (owner)
GET /ws/vnc?session_id=<id>&mode=writer|viewer  # attach / viewer
```

After opening with `target_id`, the browser resolves `session_id` via `GET /api/vnc/sessions` and enables the sharing UI (invite / participants).

### RDP (browser)

```http
GET /ws/rdp/browser?target_id=<id>&w=&h=                          # new session (owner)
GET /ws/rdp/browser?target_id=<id>&session_id=<id>&mode=viewer   # viewer (after POST /join)
```

The `target_id` on viewer attach must match the session’s target (or be omitted so the server uses the session target).

**Join before viewer attach:** consume the invitation with `POST .../join` (see API below), then open the WebSocket with `mode=viewer`. The optional `invite=` query parameter on SPA URLs is used by the frontend to call `/join` automatically.

## HTTP API (summary)

Terminal routes: `/api/terminal/sessions/{session_id}/…`

VNC and RDP use the same path suffixes under `/api/vnc/sessions/{session_id}/…` and `/api/rdp/sessions/{session_id}/…` (VNC/RDP invitations are **viewer-only**; no write-token APIs).

| Method | Path (under session prefix) | Who | Purpose |
|--------|----------------------------|-----|---------|
| `GET` | `invitation-options` | Owner | Users, tags, and groups eligible for invite |
| `POST` | `invitations` | Owner | Create invitation (`invitee_user_id`, `invite_tag`, `invite_group_id`, or link) |
| `GET` | `invitations` | Owner | List issued invitations |
| `DELETE` | `invitations/{invitation_id}` | Owner | Revoke invitation |
| `POST` | `invitations/{invitation_id}/join-url` | Owner | Regenerate link token |
| `POST` | `join` | Invitee | Consume token or named invitation id |
| `GET` | `participants` | Participant | List room members |
| `DELETE` | `participants/{user_id}` | Owner | Kick viewer |
| `POST` | `write-requests` … | Terminal only | Request / grant / release write token |

Incoming named invitations: `GET /api/invitations/incoming`

OpenAPI: [api/openapi.yaml](api/openapi.yaml)

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_INVITATION_MAX_TTL_SECONDS` | `14400` (4 h) | Maximum `ttl_seconds` when creating an invitation. Default TTL when omitted is **15 minutes**. |
| `VANTYX_REQUIRE_RECORDING_WITH_VIEWERS` | unset | When `1`/`true`/`yes`, refuse invitations and joins unless `VANTYX_RECORDINGS_DIR` is set (session recording enabled). |

See [configuration.md](configuration.md).

## Security notes

- Invitation tokens are high-entropy secrets; treat join URLs like passwords until they expire or are revoked.
- Viewers cannot inject input server-side even if the browser UI is tampered with.
- Kicking a participant or revoking an invitation closes their live WebSocket attach immediately.
- Documented in [SECURITY-ASVS-L2.md](SECURITY-ASVS-L2.md).

## Implementation map

| Layer | Location |
|-------|----------|
| Room state | `internal/sharing/registry.go` |
| Join/kick service | `internal/sharing/service.go` |
| Invitations (SQLite) | `internal/sharing/sqlite_store.go` |
| SSH/Telnet bridges | `internal/sshproxy/bridge_detachable.go`, `internal/telnetproxy/bridge_detachable.go` |
| VNC bridge | `internal/vncproxy/bridge_detachable.go` |
| HTTP handlers | `internal/httpapi/sharing.go`, `sharing_kind.go`, `sharing_join_common.go`, `vnc_sessions.go`, `rdp_sessions.go` |
| CLI | `internal/sshd/cli_sharing.go` |
| Frontend | `web/src/terminal_page.js`, `web/src/vnc_page.js`, `web/src/rdp_page.js`, `web/src/sharing_ui.js`, `web/src/invite_dialog.js`, `web/src/sharing_events.js` |

Architecture: [ARCHITECTURE.md](ARCHITECTURE.md#collaborative-sessions).
