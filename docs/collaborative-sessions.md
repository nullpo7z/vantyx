# Collaborative terminal sessions

Vantyx lets multiple logged-in users attach to the **same** SSH or Telnet
terminal session. One participant holds the **write token** (keyboard input
is forwarded to the remote host); everyone else is a **viewer** whose stdin
is dropped on the server.

Supported today: **SSH and Telnet** browser terminals only. VNC and RDP
sessions do not yet support multi-user attach (see [roadmap.md](roadmap.md)).

## Concepts

| Term | Meaning |
|------|---------|
| **Owner** | User who started the terminal session. Can invite, kick, and revoke. |
| **Writer** | Participant with the write token (usually the owner until they transfer control). |
| **Viewer** | Read-only attach; sees live output but cannot send keys unless granted the write token. |
| **Invitation** | Time-limited grant to join a session. Plain token is shown **once** at creation; only a SHA-256 hash is stored. |
| **Named invitation** | Targets a specific Vantyx user (`invitee_user_id`). Appears in their incoming-invitations list. |
| **Link invitation** | Shareable URL/token; any logged-in user who satisfies target access can join (subject to `max_uses`). |

Access to the **underlying target** is always re-checked for the **joining**
user, not the inviter. A viewer without target permission cannot join even
with a valid token.

## User workflow (SPA)

1. User A opens an SSH/Telnet session to a target (writer mode).
2. From the session UI, A creates a **named** or **link** invitation (optional TTL, optional use limit for links).
3. User B logs in, accepts the invitation (incoming list or invite link), and attaches as **viewer**.
4. B may **request control**; A (current writer) can **grant** or **deny**.
5. A can **release** the write token so another participant becomes writer, or **kick** a viewer.
6. Revoking an invitation disconnects active viewers tied to that invite.

The UI stays in sync via **SSE** (`GET /api/events/sessions`) — events such as
`participant_joined`, `write_request_pending`, `write_token_transferred`, and
`invitation_revoked`.

## WebSocket attach

Reconnect to a shared session with:

```http
GET /ws/ssh?session_id=<id>&mode=writer|viewer
```

- **Writer** — first attach on a new session, or after being granted the token.
- **Viewer** — read-only; pass `invite=<plain-token>` on the **first** attach so the server consumes the invitation atomically.

Telnet uses the same pattern on `/ws/ssh` when the underlying session protocol is Telnet.

## HTTP API (summary)

All routes require a valid session cookie unless noted. Paths are under
`/api/terminal/sessions/{session_id}/` except incoming invitations.

| Method | Path | Who | Purpose |
|--------|------|-----|---------|
| `GET` | `invitation-options` | Owner | Users/groups/tags eligible for named invites |
| `POST` | `invitations` | Owner | Create invitation (returns plain `token` once) |
| `GET` | `invitations` | Owner | List invitations for this session |
| `DELETE` | `invitations/{invitation_id}` | Owner | Revoke |
| `POST` | `invitations/{invitation_id}/join-url` | Owner | Regenerate link join URL |
| `POST` | `join` | Invitee | Consume token or invitation id |
| `GET` | `participants` | Participant | List room members + pending write requests |
| `DELETE` | `participants/{user_id}` | Owner | Kick viewer |
| `POST` | `write-requests` | Viewer | Request control |
| `POST` | `write-requests/{req_id}/grant` | Writer | Grant control |
| `POST` | `write-requests/{req_id}/deny` | Writer | Deny control |
| `POST` | `write-token/release` | Writer | Release token voluntarily |
| `GET` | `/api/invitations/incoming` | Any user | Pending named invitations for the caller |

OpenAPI: [api/openapi.yaml](api/openapi.yaml) (terminal session sharing section).

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `VANTYX_INVITATION_MAX_TTL_SECONDS` | `14400` (4 h) | Maximum `ttl_seconds` an owner may request when creating an invitation. Default TTL when omitted is **15 minutes**. |

See [configuration.md](configuration.md).

## Security notes

- Invitation tokens are high-entropy secrets; treat join URLs like passwords until they expire or are revoked.
- Viewers cannot inject keystrokes server-side even if the browser UI is tampered with.
- Collaborative invitations are documented in [SECURITY-ASVS-L2.md](SECURITY-ASVS-L2.md).

## Implementation map

| Layer | Location |
|-------|----------|
| Room state (in-memory) | `internal/sharing.Registry` |
| Invitation persistence | `session_invitations` table, `internal/sharing` SQLite store |
| Detachable SSH/Telnet bridges | `internal/sshproxy/bridge_detachable.go`, `internal/telnetproxy/bridge_detachable.go` |
| HTTP handlers | `internal/httpapi/sharing.go` |
| Frontend | `web/src/sharing_events.js`, session UI in `web/src/app.js` |

Architecture overview: [ARCHITECTURE.md](ARCHITECTURE.md#collaborative-terminal-sessions).
