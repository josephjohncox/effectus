#!/usr/bin/env bash
set -euo pipefail

BASE_URL=${EFFECTUS_URL:-"http://localhost:8080"}
TOKEN=${EFFECTUS_TOKEN:-"flow-demo-token"}
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
DATA_DIR="${SCRIPT_DIR}/../data"

send_payload() {
  local file="$1"
  echo "Streaming ${file}"
  curl -sS -X POST "${BASE_URL}/api/facts" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "@${DATA_DIR}/${file}" > /dev/null
}

send_payload "facts_stream_1.json"
sleep 1
send_payload "facts_stream_2.json"
sleep 1
send_payload "facts_stream_3.json"

echo "Streaming complete."
