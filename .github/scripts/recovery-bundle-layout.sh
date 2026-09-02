#!/bin/sh
# Select the bundle layout present in a checked-out release source tree.
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: recovery-bundle-layout.sh SOURCE_ROOT VERSION" >&2
  exit 2
fi
root=$1
version=$2

if [ -d "$root/examples/order_review/rules" ]; then
  bundle_name=order-review
  bundle_repo_path=bundles/order-review
elif [ -d "$root/examples/flow_ui_demo/rules" ]; then
  bundle_name=flow-ui-demo
  bundle_repo_path=bundles/flow-ui-demo
else
  echo "release source has no supported bundle layout: $root" >&2
  exit 1
fi

printf 'bundle_name=%s\n' "$bundle_name"
printf 'bundle_repo_path=%s\n' "$bundle_repo_path"
printf 'bundle_asset=%s-%s.json\n' "$bundle_name" "$version"
