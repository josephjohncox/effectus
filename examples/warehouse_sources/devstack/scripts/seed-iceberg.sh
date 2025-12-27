#!/usr/bin/env bash
set -euo pipefail

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

wait_for_trino

run_query "CREATE SCHEMA IF NOT EXISTS ${CATALOG}.${SCHEMA} WITH (location='s3://warehouse/${SCHEMA}')" "default"
run_query "CREATE TABLE IF NOT EXISTS ${CATALOG}.${SCHEMA}.orders (order_id varchar, amount double, updated_at timestamp) WITH (format='PARQUET')" "${SCHEMA}"
run_query "INSERT INTO ${CATALOG}.${SCHEMA}.orders VALUES ('order-1001', 19.95, TIMESTAMP '2025-01-01 00:00:00')" "${SCHEMA}"
run_query "INSERT INTO ${CATALOG}.${SCHEMA}.orders VALUES ('order-1002', 42.25, TIMESTAMP '2025-01-02 12:30:00')" "${SCHEMA}"

echo "Seeded ${CATALOG}.${SCHEMA}.orders"
