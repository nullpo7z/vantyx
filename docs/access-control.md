# Access control

Who may see and connect to a target is decided by **access groups** and
**tags**. Admins manage everything; this page describes the rules that
apply to every user, admins included, when *connecting*.

## Access groups

- A group has an ID that is a `/`-separated path, e.g. `net`, `net/tokyo`,
  `net/tokyo/rack1`. The path is the hierarchy: `net/tokyo` is a child of
  `net`. Groups are created under the group selected in the tree.
- Targets are assigned to exactly one group (the tree shows each target
  under its own group only).
- Users are made members of groups (**Server management → group →
  Members**).

### Inheritance: parent access covers descendants

Access granted on a group covers **that group and every group below it**,
never the other way round:

| User is a member of | Can reach targets in |
|---------------------|----------------------|
| `net` | `net`, `net/tokyo`, `net/tokyo/rack1`, `net/osaka`, … |
| `net/tokyo` | `net/tokyo`, `net/tokyo/rack1` — **not** `net` or `net/osaka` |

The same applies to tag grants on a group (below). Consequences:

- The tree a user sees contains their groups and all descendants.
- Adding a user to a parent group is enough for every child group created
  later; put people in deeper groups only when they should see *less*.
- "Invite the group" on a collaborative session (`net/tokyo`) reaches the
  members of `net/tokyo` **and** of its ancestors, since they hold access
  through the hierarchy. The *Members* list in server management still
  shows direct members only — that is the set you edit.
- Admins see and manage every group regardless of membership, but
  connecting still requires a membership (or tag) that reaches the target;
  a membership in a top-level group satisfies that for everything below it.

Only the `/` separator defines the hierarchy: `net_x` is a sibling of
`net`, not a child (IDs may contain `_`).

## Tags

Tags are a second, orthogonal grant. A user with tag `T` can reach:

- every target that carries tag `T`;
- every target in a group that carries tag `T` — **and in that group's
  descendants**, the same inheritance rule as membership.

Each group gets a default tag on creation (`net/tokyo` → `net_tokyo`). Tags
are 1–64 characters of letters, digits, `-` and `_`.

## Where the rules are enforced

All checks go through `internal/access.AccessGroupStore`
(`TargetIDsForUser`, `GroupIDsForUser`, `UserIDsForTarget`,
`TagsGrantingTargetAccess`) so the web UI, the WebSocket bridges, the CLI
gateway and collaborative-session invitations agree. The hierarchy test is
a prefix comparison on the group ID (`substr(child, 1, len(parent)+1) =
parent || '/'`), not `LIKE`, so `_` in IDs is never a wildcard.
