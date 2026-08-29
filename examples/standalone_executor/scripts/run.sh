#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ROOT_DIR="$(cd "$EXAMPLE_DIR/../.." && pwd)"
COMPOSE=(docker compose -f "$EXAMPLE_DIR/docker-compose.yml")
BUNDLE="$ROOT_DIR/out/standalone_executor/bundle.json"
EFFECTUS_TOKEN="${EFFECTUS_API_TOKEN:-effectus-demo-token}"
EXECUTOR_DEMO_TOKEN="${EXECUTOR_TOKEN:-local-example-only}"

for command in docker go curl python3; do
  command -v "$command" >/dev/null || {
    echo "missing required command: $command" >&2
    exit 1
  }
done

mkdir -p "$(dirname "$BUNDLE")"
(
  cd "$ROOT_DIR"
  go run ./cmd/effectusc bundle \
    --name order-review \
    --version 1.0.0 \
    --schema-dir examples/standalone_executor/schema \
    --verb-dir examples/standalone_executor/verbs \
    --verbschema examples/standalone_executor/schema/order_verbs.json \
    --rules-dir examples/standalone_executor/rules \
    --output "$BUNDLE"
)

"${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
if ! "${COMPOSE[@]}" up -d --build; then
  "${COMPOSE[@]}" logs --no-color
  exit 1
fi

ready=0
for _ in $(seq 1 90); do
  if curl --fail --silent http://127.0.0.1:18080/readyz >/dev/null \
    && curl --fail --silent http://127.0.0.1:8090/healthz >/dev/null; then
    ready=1
    break
  fi
  sleep 1
done
if [[ "$ready" != 1 ]]; then
  "${COMPOSE[@]}" logs --no-color
  echo "demo services did not become ready" >&2
  exit 1
fi

submit() {
  curl --fail-with-body --silent \
    --request POST http://127.0.0.1:18080/api/facts \
    --header "Authorization: Bearer $EFFECTUS_TOKEN" \
    --header 'Idempotency-Key: order-200-created' \
    --header 'Content-Type: application/json' \
    --data @"$EXAMPLE_DIR/data/order.json"
}

first="$(submit)"
second="$(submit)"
FIRST="$first" SECOND="$second" python3 - <<'PY'
import json
import os

first = json.loads(os.environ["FIRST"])
second = json.loads(os.environ["SECOND"])
assert first["execution_id"] == second["execution_id"]
print(json.dumps(first, indent=2, sort_keys=True))
PY

state=''
for _ in $(seq 1 60); do
  state="$(curl --fail --silent \
    --header "X-Demo-Token: $EXECUTOR_DEMO_TOKEN" \
    http://127.0.0.1:8090/reviews)"
  if STATE="$state" python3 - <<'PY'
import json
import os

raise SystemExit(0 if len(json.loads(os.environ["STATE"])["reviews"]) == 1 else 1)
PY
  then
    break
  fi
  sleep 1
done

STATE="$state" python3 - <<'PY'
import json
import os

state = json.loads(os.environ["STATE"])
assert len(state["reviews"]) == 1, state
print(json.dumps(state, indent=2, sort_keys=True))
PY

echo "OK standalone effectusd and business executor demo passed"
echo "UI: http://127.0.0.1:18080/ui"
echo "Stop: examples/standalone_executor/scripts/down.sh"
