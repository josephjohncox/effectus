#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT

assert_layout() {
  root=$1
  expected=$2
  output=$("$script_dir/recovery-bundle-layout.sh" "$root" 0.3.0)
  case "$output" in
    *"bundle_name=$expected"*"bundle_repo_path=bundles/$expected"*"bundle_asset=$expected-0.3.0.json") ;;
    *)
      echo "unexpected recovery layout: $output" >&2
      exit 1
      ;;
  esac
}

legacy=$temp/legacy
mkdir -p "$legacy/examples/flow_ui_demo/rules"
if "$script_dir/recovery-bundle-layout.sh" "$legacy" 0.3.0 >/dev/null 2>&1; then
  echo "removed flow-ui-demo layout was accepted" >&2
  exit 1
fi

current=$temp/current
mkdir -p "$current/examples/order_review/rules"
assert_layout "$current" order-review

if "$script_dir/recovery-bundle-layout.sh" "$temp/missing" 0.3.0 >/dev/null 2>&1; then
  echo "missing release layout was accepted" >&2
  exit 1
fi
