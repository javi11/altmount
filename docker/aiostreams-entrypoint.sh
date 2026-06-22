#!/bin/sh
set -e

INIT_FILE="/config/shared/aiostreams-init.json"

if [ -f "$INIT_FILE" ]; then
  # Pure-sh JSON parse (no external dependencies needed)
  ALTMOUNT_URL=$(grep -o '"url":"[^"]*"' "$INIT_FILE" | head -1 | cut -d'"' -f4)
  ALTMOUNT_API_KEY=$(grep -o '"api_key":"[^"]*"' "$INIT_FILE" | head -1 | cut -d'"' -f4)

  if [ -n "$ALTMOUNT_URL" ]; then
    export ALTMOUNT_URL
    export ALTMOUNT_API_KEY
    printf '[aiostreams-entrypoint] Loaded AltMount config from %s\n' "$INIT_FILE"
  else
    printf '[aiostreams-entrypoint] WARN: init file found but .url is empty\n'
  fi
else
  printf '[aiostreams-entrypoint] WARN: %s not found, starting without AltMount config\n' "$INIT_FILE"
fi

# Hand off to AIOStreams' default start command.
# Adjust if AIOStreams image uses a different CMD (check: docker inspect ghcr.io/viren070/aiostreams:latest)
exec node /app/dist/index.js "$@"
