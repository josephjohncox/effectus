#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
export COMPOSE_PROJECT_NAME="${EFFECTUS_DEMO_PROJECT:-standalone_executor}"
export EFFECTUS_API_TOKEN="${EFFECTUS_API_TOKEN:-}"
export EXECUTOR_TOKEN="${EXECUTOR_TOKEN:-}"
export EFFECTUS_DEMO_HTTP_PORT="${EFFECTUS_DEMO_HTTP_PORT:-18080}"
export EXECUTOR_DEMO_HTTP_PORT="${EXECUTOR_DEMO_HTTP_PORT:-8090}"

docker compose -f "$EXAMPLE_DIR/docker-compose.yml" down \
  --volumes --remove-orphans --timeout 10
