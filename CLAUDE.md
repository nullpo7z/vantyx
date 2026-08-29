# Vantyx — deploy notes

## Deploying to production

Always use `scripts/deploy.sh` (or copy its exact rsync flags) — never a bare
`rsync -az --delete ./ user@host:/opt/vantyx/` typed from memory.

```
scripts/deploy.sh                      # defaults to nullpo7z@10.10.10.70:/opt/vantyx
```

This rsyncs the repo to the server, then runs
`docker compose -f docker-compose.dev.yml up --build -d` **on the server** so
the image is always built there from source.

- **Never** run plain `docker compose up` / `docker compose build` against
  `docker-compose.yml` for a deploy — that file pulls the public
  `nullpo7z/vantyx:latest` image from Docker Hub instead of building your
  changes.
- **Never** push to the public Docker Hub image. Only build locally on the
  target host.

### Why `docker-compose.yml` / `docker-compose.dev.yml` / `.env` are excluded from rsync

Incident (2026-08-25): a plain `rsync -az --delete ./ server:/opt/vantyx/`
(no excludes for these files) overwrote the server's `docker-compose.yml` and
`docker-compose.dev.yml` with the repo's checked-in *template* versions
(placeholder volume paths like `/path/to/vantyx/data`). Docker then silently
auto-created empty directories at that literal placeholder path and mounted
those instead of the real data, and the container crash-looped on a
permission error. The server's compose files are **not** tracked by git
there (`/opt/vantyx` isn't a git repo) — they hold real, per-deployment
values that only exist on the server, so a git-based diff never warns you
before they get clobbered.

Same reasoning applies to `.env` (real secrets/host config) — it must never
be replaced by a local copy.

`scripts/deploy.sh` excludes all of these. If you ever write the rsync
command by hand instead, always exclude:
`docker-compose.yml`, `docker-compose.dev.yml`, `.env`, `.env.*`.

### Real runtime paths on the production host (10.10.10.70)

- `/app/certs` → `/home/nullpo7z/vantyx-runtime/certs` (local disk)
- `/app/data` → `/home/nullpo7z/vantyx-runtime/data` (local disk — **must not**
  be a NAS/CIFS mount: SQLite over CIFS throws `SQLITE_BUSY`/`database is
  locked` because network filesystems don't support the locking SQLite
  needs)
- `/app/recordings` → `/mnt/nas/serverdata/vantyx/recordings` (NAS/CIFS is
  fine here — plain sequential file writes, no locking)
- The compose files set `user: "1000:1000"` to override the image's default
  `USER nonroot` (uid 65532): the recordings NAS mount only grants write
  access to uid 1000 (or root), not 65532, so the container must run as
  1000:1000 on this host. If the mount/NAS permissions ever change, revisit
  whether this override is still needed.

## Verifying frontend changes before deploy

No local Node/Go toolchain is available in this dev environment. Verify
frontend changes with a scoped Docker build instead of assuming syntax is
correct:

```
docker build --target frontend -t vantyx-frontend-verify .
docker image rm vantyx-frontend-verify
```

For Go changes, use `docker build --target builder` and
`docker run <image> go test ./...`, then `docker image rm` to clean up.
