#!/usr/bin/env bash
# Deploys the current working tree to the production host by rsyncing the
# repo and rebuilding the image locally there (never pulls/pushes the public
# Docker Hub image for deploys).
#
# Usage: scripts/deploy.sh [user@host] [remote_dir]
# Defaults match the current production host.
set -eo pipefail

REMOTE="${1:-nullpo7z@10.10.10.70}"
REMOTE_DIR="${2:-/opt/vantyx}"

# --delete makes this sync exact, so anything server-specific that also
# happens to exist (even untracked/gitignored) in the local tree WILL be
# overwritten on the server. docker-compose.yml / docker-compose.dev.yml
# and .env hold real per-deployment values (volume host paths, secrets)
# that must never be replaced by the repo's checked-in template versions.
# See CLAUDE.md "Deploying" for the incident this guards against.
EXCLUDES=(
  --exclude '.git'
  --exclude 'node_modules'
  --exclude 'web/node_modules'
  --exclude 'web/dist'
  --exclude 'docker-compose.yml'
  --exclude 'docker-compose.dev.yml'
  --exclude '.env'
  --exclude '.env.*'
  --exclude 'cmd/vantyx-server/data/'
)

echo "==> rsyncing to ${REMOTE}:${REMOTE_DIR}"
rsync -az --delete "${EXCLUDES[@]}" ./ "${REMOTE}:${REMOTE_DIR}/"

echo "==> building and starting on ${REMOTE} (local build, docker-compose.dev.yml)"
ssh "${REMOTE}" "cd ${REMOTE_DIR} && docker compose -f docker-compose.dev.yml up --build -d"

echo "==> done. recent logs:"
ssh "${REMOTE}" "docker logs vantyx-vantyx-1 --tail 10"
