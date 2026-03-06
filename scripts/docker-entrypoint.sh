#!/bin/sh
# Ensure /app/certs and /app/data are writable by the app user (e.g. when using Docker volumes)
chown -R nonroot:nonroot /app/certs /app/data 2>/dev/null || true
exec su-exec nonroot /app/vantyx
