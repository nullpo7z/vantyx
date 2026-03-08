#!/bin/sh
# Ensure volume mount points are writable by the app user (e.g. when using Docker volumes)
for d in /app/certs /app/data /app/recordings; do
  [ -d "$d" ] && chown -R nonroot:nonroot "$d" 2>/dev/null || true
done
exec su-exec nonroot /app/vantyx
