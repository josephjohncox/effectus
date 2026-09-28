#!/bin/sh
# shellcheck disable=SC2016 # Match literal workflow variable expressions.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
policy=$script_dir/recovery-compat-policy.sh
workflow=$script_dir/../workflows/recover-release.yml

for version in 0.1.0 0.2.0 0.2.1 0.3.0 0.4.0-rc.1; do
  test "$("$policy" "$version")" = not-applicable
done
for version in 0.4.0 0.4.1-rc.1 0.5.0 0.6.1 1.0.0-alpha.1; do
  test "$("$policy" "$version")" = required
done
if "$policy" v0.4.0 >/dev/null 2>&1 ||
  "$policy" 0.4 >/dev/null 2>&1; then
  echo 'recovery compatibility policy accepted an invalid version' >&2
  exit 1
fi

grep -Fq '.recovery-helper/.github/scripts/recovery-compat-policy.sh "$VERSION"' "$workflow"
grep -Fq '.recovery-helper/scripts/compat-proxy-smoke.sh "$VERSION"' "$workflow"
if grep -Eq '^[[:space:]]+if scripts/compat-proxy-smoke.sh' "$workflow"; then
  echo 'recovery uses a smoke script that can be absent at the release tag' >&2
  exit 1
fi
