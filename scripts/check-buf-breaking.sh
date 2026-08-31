#!/usr/bin/env bash
set -euo pipefail

against="${1:-.git#branch=main}"
set +e
output="$(buf breaking --against "$against" 2>&1)"
status=$?
set -e
if [[ "$status" -eq 0 ]]; then
  exit 0
fi

expected="$(cat <<'EOF'
effectus/v1/common.proto:7:1:File option "go_package" changed from "github.com/effectus/effectus-go/gen/effectus/v1;effectusv1" to "github.com/josephjohncox/effectus/gen/effectus/v1;effectusv1".
effectus/v1/execution.proto:10:1:File option "go_package" changed from "github.com/effectus/effectus-go/gen/effectus/v1;effectusv1" to "github.com/josephjohncox/effectus/gen/effectus/v1;effectusv1".
effectus/v1/facts.proto:8:1:File option "go_package" changed from "github.com/effectus/effectus-go/gen/effectus/v1;effectusv1" to "github.com/josephjohncox/effectus/gen/effectus/v1;effectusv1".
effectus/v1/ir.proto:5:1:File option "go_package" changed from "github.com/effectus/effectus-go/gen/effectus/v1;effectusv1" to "github.com/josephjohncox/effectus/gen/effectus/v1;effectusv1".
effectus/v1/verbs.proto:9:1:File option "go_package" changed from "github.com/effectus/effectus-go/gen/effectus/v1;effectusv1" to "github.com/josephjohncox/effectus/gen/effectus/v1;effectusv1".
runtime/ruleset_execution.proto:8:1:File option "go_package" changed from "github.com/effectus/effectus-go/runtime" to "github.com/josephjohncox/effectus/runtime".
EOF
)"

if diff -u \
  <(printf '%s\n' "$expected" | LC_ALL=C sort) \
  <(printf '%s\n' "$output" | awk 'NF' | LC_ALL=C sort); then
  echo "Buf compatibility check accepted the allowlisted public Go module migration."
  exit 0
fi

printf '%s\n' "$output" >&2
exit "$status"
