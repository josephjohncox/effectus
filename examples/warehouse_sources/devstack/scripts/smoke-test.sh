#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STACK_FILE="${ROOT_DIR}/docker-compose.yml"
TRINO_URL="${TRINO_URL:-http://localhost:8080}"
TRINO_USER="${TRINO_USER:-effectus}"
CATALOG="${CATALOG:-lakehouse}"
SCHEMA="${SCHEMA:-sales}"

wait_for_trino() {
  echo "Waiting for Trino at ${TRINO_URL}..."
  for _ in $(seq 1 30); do
    if curl -s "${TRINO_URL}/v1/info" >/dev/null; then
      return 0
    fi
    sleep 2
  done
  echo "Trino did not become ready in time."
  exit 1
}

run_query() {
  local query="$1"
  local schema="$2"
  local resp next

  resp=$(curl -s -X POST "${TRINO_URL}/v1/statement" \
    -H "X-Trino-User: ${TRINO_USER}" \
    -H "X-Trino-Catalog: ${CATALOG}" \
    -H "X-Trino-Schema: ${schema}" \
    --data "${query}")

  next=$(echo "${resp}" | sed -n 's/.*"nextUri":"\([^"]*\)".*/\1/p')
  while [ -n "${next}" ]; do
    resp=$(curl -s "${next}")
    next=$(echo "${resp}" | sed -n 's/.*"nextUri":"\([^"]*\)".*/\1/p')
  done
}

echo "Starting warehouse devstack..."
docker compose -f "${STACK_FILE}" up -d

wait_for_trino

"${ROOT_DIR}/scripts/seed-iceberg.sh"

run_query "SELECT COUNT(*) FROM orders" "${SCHEMA}"

echo "Smoke test OK"
