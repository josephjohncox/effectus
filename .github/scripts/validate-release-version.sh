#!/bin/sh
# Validate the shared publish and recovery release-version policy.
set -eu

usage() {
  echo "usage: validate-release-version.sh [--tag] VERSION" >&2
  exit 2
}

mode=version
if [ "${1:-}" = "--tag" ]; then
  mode=tag
  shift
fi
[ "$#" -eq 1 ] || usage
value=$1

if [ "$mode" = tag ]; then
  case "$value" in
    v*) value=${value#v} ;;
    *) echo "release tag must start with v: $1" >&2; exit 1 ;;
  esac
fi

# Keep prerelease acceptance identical for publish and recovery. Build metadata
# is intentionally not a release coordinate in this repository. grep matches
# individual lines, so reject embedded newlines before applying the expression.
case "$value" in
  *'
'*)
    echo "release version is not semantic: $1" >&2
    exit 1
    ;;
esac
if ! printf '%s\n' "$value" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*))?$'; then
  echo "release version is not semantic: $1" >&2
  exit 1
fi
