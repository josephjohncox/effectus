#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

cp "$EXAMPLE_DIR/manifest.v1.json" "$EXAMPLE_DIR/manifest.json"

(cd "$ROOT_DIR" && go run ./examples/multi_bundle_runtime --watch --interval 2s) &
PID=$!

trap 'kill $PID >/dev/null 2>&1 || true' EXIT

sleep 4
cp "$EXAMPLE_DIR/manifest.v2.json" "$EXAMPLE_DIR/manifest.json"

sleep 6
