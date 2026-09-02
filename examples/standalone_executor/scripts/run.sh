#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ROOT_DIR="$(cd "$EXAMPLE_DIR/../.." && pwd)"
export COMPOSE_PROJECT_NAME="${EFFECTUS_DEMO_PROJECT:-standalone_executor}"
export EFFECTUS_API_TOKEN="${EFFECTUS_API_TOKEN:-effectus-demo-token}"
export EXECUTOR_TOKEN="${EXECUTOR_TOKEN:-executor-demo-token}"
export EFFECTUS_DEMO_HTTP_PORT="${EFFECTUS_DEMO_HTTP_PORT:-18080}"
export EXECUTOR_DEMO_HTTP_PORT="${EXECUTOR_DEMO_HTTP_PORT:-8090}"
# Compose builds this image from the checked-out source tree. A caller can
# change only the local tag, not substitute a prebuilt release image.
EFFECTUS_IMAGE="${EFFECTUS_IMAGE:-effectus-demo-current}"
export EFFECTUS_IMAGE
COMPOSE=(docker compose -f "$EXAMPLE_DIR/docker-compose.yml")
BUNDLE="$ROOT_DIR/out/standalone_executor/bundle.json"
RUNTIME_EXTENSIONS="$ROOT_DIR/out/standalone_executor/extensions"
ORDER_SCENARIO="$ROOT_DIR/examples/order_review/data/order.json"
ORDER_REQUEST=""
IDEMPOTENCY_KEY=""
CONFLICT_REQUEST=""
CONFLICT_BODY=""
STACK_CREATED=0
SUCCESS=0

cleanup() {
  local status=$?
  trap - EXIT INT TERM HUP
  for temporary_file in "$ORDER_REQUEST" "$CONFLICT_REQUEST" "$CONFLICT_BODY"; do
    if [[ -n "$temporary_file" ]]; then
      rm -f "$temporary_file"
    fi
  done
  if [[ "$SUCCESS" != 1 && "$STACK_CREATED" == 1 ]]; then
    echo "The durable demo failed. Compose logs follow." >&2
    "${COMPOSE[@]}" logs --no-color --tail=200 >&2 || true
    "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

for command in docker curl python3 go; do
  command -v "$command" >/dev/null 2>&1 || fail "missing required command: $command"
done
[[ -n "${BASH_VERSION:-}" ]] || fail "run this script with Bash"
docker compose version >/dev/null 2>&1 || fail "Docker Compose is not available"
docker info >/dev/null 2>&1 || fail "the Docker daemon is not available"

validate_port() {
  local name=$1
  local port=$2
  if [[ ! "$port" =~ ^(0|[1-9][0-9]*)$ ]] || ((port < 1 || port > 65535)); then
    fail "$name must be an integer from 1 through 65535"
  fi
}
validate_port EFFECTUS_DEMO_HTTP_PORT "$EFFECTUS_DEMO_HTTP_PORT"
validate_port EXECUTOR_DEMO_HTTP_PORT "$EXECUTOR_DEMO_HTTP_PORT"
[[ "$EFFECTUS_DEMO_HTTP_PORT" != "$EXECUTOR_DEMO_HTTP_PORT" ]] ||
  fail "EFFECTUS_DEMO_HTTP_PORT and EXECUTOR_DEMO_HTTP_PORT must be different"

compose_config="$("${COMPOSE[@]}" config --format json)"
existing_containers="$(docker ps --all --quiet \
  --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME")"
existing_networks="$(docker network ls --quiet \
  --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME")"
existing_volumes="$(docker volume ls --quiet \
  --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME")"
expected_networks="$(python3 -c \
  'import json, sys; print("\n".join(item["name"] for item in json.load(sys.stdin).get("networks", {}).values()))' \
  <<<"$compose_config")"
expected_volumes="$(python3 -c \
  'import json, sys; print("\n".join(item["name"] for item in json.load(sys.stdin).get("volumes", {}).values()))' \
  <<<"$compose_config")"
for network in $expected_networks; do
  if docker network inspect "$network" >/dev/null 2>&1; then
    existing_networks+=" $network"
  fi
done
for volume in $expected_volumes; do
  if docker volume inspect "$volume" >/dev/null 2>&1; then
    existing_volumes+=" $volume"
  fi
done
if [[ -n "$existing_containers" || -n "$existing_networks" || -n "$existing_volumes" ]]; then
  fail "Compose project $COMPOSE_PROJECT_NAME already has resources; preserve them or explicitly reset them with examples/standalone_executor/scripts/down.sh"
fi

python3 - "$EFFECTUS_DEMO_HTTP_PORT" "$EXECUTOR_DEMO_HTTP_PORT" <<'PY'
import socket
import sys

for raw_port in sys.argv[1:]:
    port = int(raw_port)
    probe = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        probe.bind(("127.0.0.1", port))
    except OSError as error:
        raise SystemExit(f"ERROR: host port 127.0.0.1:{port} is not available: {error}")
    finally:
        probe.close()
PY

ORDER_REQUEST="$(mktemp)"
IDEMPOTENCY_KEY="$(python3 - "$ORDER_SCENARIO" "$ORDER_REQUEST" <<'PY'
import json
import sys

with open(sys.argv[1]) as source:
    scenario = json.load(source)
idempotency_key = scenario.get("idempotency_key")
request = scenario.get("request")
if not isinstance(idempotency_key, str) or not idempotency_key or "\n" in idempotency_key or "\r" in idempotency_key:
    raise SystemExit("ERROR: scenario idempotency_key must be a nonempty single-line string")
if not isinstance(request, dict):
    raise SystemExit("ERROR: scenario request must be an object")
with open(sys.argv[2], "w") as target:
    json.dump(request, target)
    target.write("\n")
print(idempotency_key)
PY
)"

mkdir -p "$(dirname "$BUNDLE")"
rm -rf "$RUNTIME_EXTENSIONS"
mkdir -p "$RUNTIME_EXTENSIONS"
EXECUTOR_DEMO_TOKEN="$EXECUTOR_TOKEN" python3 - \
  "$EXAMPLE_DIR/extensions" "$RUNTIME_EXTENSIONS" <<'PY'
import json
import os
import shutil
import sys
from pathlib import Path

source_dir = Path(sys.argv[1])
target_dir = Path(sys.argv[2])
token = os.environ["EXECUTOR_DEMO_TOKEN"]
replacements = 0
for source in sorted(source_dir.glob("*.json")):
    target = target_dir / source.name
    if source.name != "order.verbs.json":
        shutil.copyfile(source, target)
        continue
    payload = json.loads(source.read_text())
    for verb in payload["verbs"]:
        headers = verb["target"]["config"].get("headers", {})
        if headers.get("X-Demo-Token") != "__EXECUTOR_TOKEN__":
            raise SystemExit(f"unexpected executor token template in {source}")
        headers["X-Demo-Token"] = token
        replacements += 1
    target.write_text(json.dumps(payload, indent=2) + "\n")
if replacements != 2:
    raise SystemExit(f"expected two executor token replacements, got {replacements}")
PY

(
  cd "$ROOT_DIR"
  go run ./cmd/effectusc bundle \
    --name order-review \
    --version 1.0.0 \
    --schema-dir examples/standalone_executor/schema \
    --verb-dir out/standalone_executor/extensions \
    --rules-dir examples/order_review/rules \
    --output out/standalone_executor/bundle.json
)

test -s "$BUNDLE" || fail "effectusc did not create $BUNDLE"
# No project resources existed above, so any resources created after this point
# belong to this invocation and are safe to remove on failure.
STACK_CREATED=1
"${COMPOSE[@]}" up -d --build

wait_until_ready() {
  local ready=0
  for _ in $(seq 1 90); do
    if curl --fail --silent "http://127.0.0.1:${EFFECTUS_DEMO_HTTP_PORT}/readyz" >/dev/null &&
      curl --fail --silent "http://127.0.0.1:${EXECUTOR_DEMO_HTTP_PORT}/healthz" >/dev/null; then
      ready=1
      break
    fi
    sleep 1
  done
  [[ "$ready" == 1 ]] || fail "demo services did not become ready"
}
wait_until_ready

submit() {
  curl --fail-with-body --silent \
    --request POST "http://127.0.0.1:${EFFECTUS_DEMO_HTTP_PORT}/api/facts" \
    --header "Authorization: Bearer $EFFECTUS_API_TOKEN" \
    --header "Idempotency-Key: $IDEMPOTENCY_KEY" \
    --header 'Content-Type: application/json' \
    --data @"$ORDER_REQUEST"
}

first="$(submit)"
state=''
for _ in $(seq 1 60); do
  state="$(curl --fail --silent \
    --header "X-Demo-Token: $EXECUTOR_TOKEN" \
    "http://127.0.0.1:${EXECUTOR_DEMO_HTTP_PORT}/reviews")"
  if STATE="$state" python3 - <<'PY'; then
import json
import os

raise SystemExit(0 if len(json.loads(os.environ["STATE"])["reviews"]) == 1 else 1)
PY
    break
  fi
  sleep 1
done
STATE="$state" python3 - <<'PY'
import json
import os

state = json.loads(os.environ["STATE"])
assert len(state["reviews"]) == 1, state
PY

"${COMPOSE[@]}" restart effectusd business-executor >/dev/null
wait_until_ready
second="$(submit)"
state="$(curl --fail --silent \
  --header "X-Demo-Token: $EXECUTOR_TOKEN" \
  "http://127.0.0.1:${EXECUTOR_DEMO_HTTP_PORT}/reviews")"
FIRST="$first" SECOND="$second" STATE="$state" python3 - <<'PY'
import json
import os

first = json.loads(os.environ["FIRST"])
second = json.loads(os.environ["SECOND"])
state = json.loads(os.environ["STATE"])
assert first["execution_id"] == second["execution_id"], (first, second)
assert len(state["reviews"]) == 1, state
print(json.dumps({
    "execution_id": first["execution_id"],
    "replayed_execution_id": second["execution_id"],
    "replay_ids_match": True,
    "review_count": 1,
}, indent=2, sort_keys=True))
PY

CONFLICT_REQUEST="$(mktemp)"
python3 - "$ORDER_REQUEST" "$CONFLICT_REQUEST" <<'PY'
import json
import sys

with open(sys.argv[1]) as source:
    request = json.load(source)
request["facts"]["order"]["risk_score"] = 83
with open(sys.argv[2], "w") as target:
    json.dump(request, target)
PY
CONFLICT_BODY="$(mktemp)"
conflict_status="$(curl --silent \
  --output "$CONFLICT_BODY" \
  --write-out '%{http_code}' \
  --request POST "http://127.0.0.1:${EFFECTUS_DEMO_HTTP_PORT}/api/facts" \
  --header "Authorization: Bearer $EFFECTUS_API_TOKEN" \
  --header "Idempotency-Key: $IDEMPOTENCY_KEY" \
  --header 'Content-Type: application/json' \
  --data @"$CONFLICT_REQUEST")"
[[ "$conflict_status" == 409 ]] || {
  cat "$CONFLICT_BODY" >&2
  fail "conflicting replay returned HTTP $conflict_status, want 409"
}
echo "conflicting_replay_http_status: $conflict_status"
cat "$CONFLICT_BODY"
rm -f "$CONFLICT_BODY"
CONFLICT_BODY=""

SUCCESS=1
echo "OK durable order-review demo passed"
echo "UI: http://127.0.0.1:${EFFECTUS_DEMO_HTTP_PORT}/ui"
echo "Logs: docker compose -f examples/standalone_executor/docker-compose.yml logs"
echo "Stop and delete data: examples/standalone_executor/scripts/down.sh"
