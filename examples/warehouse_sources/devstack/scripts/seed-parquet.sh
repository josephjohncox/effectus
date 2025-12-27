#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../../.." && pwd)"
OUTPUT_PATH="${ROOT_DIR}/data/events.parquet"

go run "${REPO_ROOT}/examples/warehouse_sources/devstack/scripts/seed-parquet.go" "${OUTPUT_PATH}"

docker compose -f "${ROOT_DIR}/docker-compose.yml" exec -T minio-mc \
  mc alias set local http://minio:9000 minioadmin minioadmin >/dev/null

docker compose -f "${ROOT_DIR}/docker-compose.yml" exec -T minio-mc \
  mc cp /seed/events.parquet local/exports/parquet/events.parquet

echo "Uploaded events.parquet to s3://exports/parquet/"
