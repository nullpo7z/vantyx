#!/bin/sh
# Ensure /app/certs is writable by the app user (e.g. when using a fresh Docker volume)
chown -R nonroot:nonroot /app/certs 2>/dev/null || true
exec su-exec nonroot /app/vantyx
