#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

COMPOSE_FILE="$EXAMPLE_DIR/docker-compose.yml"

docker compose -f "$COMPOSE_FILE" up -d --build

RISK_URL="http://localhost:8081/cases" \
NOTIFY_URL="http://localhost:8082/notify" \
  (cd "$EXAMPLE_DIR" && go run .)
