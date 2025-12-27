#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/docker-compose.yml"

docker compose -f "${COMPOSE_FILE}" exec -T minio-mc \
  mc alias set local http://minio:9000 minioadmin minioadmin >/dev/null

docker compose -f "${COMPOSE_FILE}" exec -T minio-mc \
  mc cp /seed/events.ndjson local/exports/events/events.ndjson

echo "Uploaded events.ndjson to s3://exports/events/"
