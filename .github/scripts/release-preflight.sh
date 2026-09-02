#!/bin/sh
set -eu

if [ "$#" -ne 4 ]; then
  echo "usage: release-preflight.sh VERSION IMAGE_REF BUNDLE_REF CHART_REF" >&2
  exit 2
fi
version=$1
image_ref=$2
bundle_ref=$3
chart_ref=$4

if [ "${GITHUB_REF_TYPE:-}" != "tag" ] || [ "${GITHUB_REF_NAME:-}" != "v$version" ]; then
  echo "release source must be the v$version tag" >&2
  exit 1
fi
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
"$script_dir/validate-release-version.sh" "$version"
bundle_prefix="ghcr.io/"
bundle_suffix="/bundles/order-review:$version"
case "$bundle_ref" in
  "$bundle_prefix"*"$bundle_suffix")
    bundle_owner=${bundle_ref#"$bundle_prefix"}
    bundle_owner=${bundle_owner%"$bundle_suffix"}
    ;;
  *)
    echo "release bundle must use ghcr.io/OWNER/bundles/order-review:$version" >&2
    exit 1
    ;;
esac
# GHCR owners are exactly one lowercase repository path component. Checking
# the extracted owner, rather than a glob, prevents empty and nested owners.
if ! printf '%s\n' "$bundle_owner" | grep -Eq '^[a-z0-9]+([._-][a-z0-9]+)*$'; then
  echo "release bundle must use a single lowercase ghcr.io owner" >&2
  exit 1
fi

chart_version=$(awk '$1 == "version:" { print $2; exit }' charts/effectusd/Chart.yaml)
chart_app_version=$(awk '$1 == "appVersion:" { gsub(/"/, "", $2); print $2; exit }' charts/effectusd/Chart.yaml)
if [ "$chart_version" != "$version" ] || [ "$chart_app_version" != "$version" ]; then
  echo "Helm chart version does not match release version $version" >&2
  exit 1
fi
if ! node -e '
  const expected = process.argv[1];
  const manifest = require("./tools/vscode-extension/package.json");
  const lock = require("./tools/vscode-extension/package-lock.json");
  if (manifest.version !== expected || lock.version !== expected || lock.packages[""].version !== expected) process.exit(1);
' "$version"; then
  echo "VS Code package versions do not match release version $version" >&2
  exit 1
fi
notes="docs/releases/v${version}.md"
if [ ! -s "$notes" ]; then
  echo "release notes are missing: $notes" >&2
  exit 1
fi

if [ "$(git rev-parse "refs/tags/${GITHUB_REF_NAME}^{commit}")" != "$(git rev-parse HEAD)" ]; then
  echo "checked out SHA does not match the release tag" >&2
  exit 1
fi
git fetch --no-tags origin main:refs/remotes/origin/main
if ! git merge-base --is-ancestor HEAD origin/main; then
  echo "release commit is not contained in protected main" >&2
  exit 1
fi

check_absent() {
  ref=$1
  error_file=$(mktemp)
  if crane digest "$ref" >/dev/null 2>"$error_file"; then
    echo "release reference already exists: $ref" >&2
    rm -f "$error_file"
    exit 1
  fi
  if ! grep -Eqi 'MANIFEST_UNKNOWN|NAME_UNKNOWN|manifest unknown|not found|404' "$error_file"; then
    echo "cannot prove release reference is absent: $ref" >&2
    cat "$error_file" >&2
    rm -f "$error_file"
    exit 1
  fi
  rm -f "$error_file"
}

check_absent "$image_ref"
check_absent "$bundle_ref"
check_absent "$chart_ref"

release_error=$(mktemp)
if gh release view "$GITHUB_REF_NAME" >/dev/null 2>"$release_error"; then
  echo "GitHub release already exists: $GITHUB_REF_NAME" >&2
  rm -f "$release_error"
  exit 1
fi
if ! grep -Eqi 'release not found|HTTP 404' "$release_error"; then
  echo "cannot prove GitHub release is absent" >&2
  cat "$release_error" >&2
  rm -f "$release_error"
  exit 1
fi
rm -f "$release_error"
