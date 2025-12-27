#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

COMPOSE_FILE="$EXAMPLE_DIR/docker-compose.yml"

docker compose -f "$COMPOSE_FILE" down
