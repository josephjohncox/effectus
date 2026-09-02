#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT

cat >"$temp/git" <<'EOF'
#!/bin/sh
case "$1" in
  rev-parse) echo abc ;;
  fetch) exit 0 ;;
  merge-base) exit 0 ;;
  *) exit 1 ;;
esac
EOF
cat >"$temp/gh" <<'EOF'
#!/bin/sh
echo "release not found" >&2
exit 1
EOF
chmod +x "$temp/git" "$temp/gh"

cat >"$temp/crane" <<'EOF'
#!/bin/sh
echo "MANIFEST_UNKNOWN: manifest unknown" >&2
exit 1
EOF
chmod +x "$temp/crane"

version=$(awk '$1 == "version:" { print $2; exit }' charts/effectusd/Chart.yaml)
bundle="ghcr.io/example/bundles/order-review:$version"

# Publish and recovery share this policy. Prereleases remain recoverable.
"$script_dir/validate-release-version.sh" 0.3.0-rc.1
"$script_dir/validate-release-version.sh" --tag v0.3.0-rc.1
for malformed in v0.3 v0.3.0- v0.3.0-rc..1 0.3.0; do
  if "$script_dir/validate-release-version.sh" --tag "$malformed" >/dev/null 2>&1; then
    echo "malformed recovery tag passed version policy: $malformed" >&2
    exit 1
  fi
done
for malformed in 01.2.3 1.02.3 1.2.03 1.2.3-01 1.2.3-rc.01; do
  if "$script_dir/validate-release-version.sh" "$malformed" >/dev/null 2>&1; then
    echo "invalid semantic version passed version policy: $malformed" >&2
    exit 1
  fi
  if "$script_dir/validate-release-version.sh" --tag "v$malformed" >/dev/null 2>&1; then
    echo "invalid semantic tag passed version policy: v$malformed" >&2
    exit 1
  fi
done
multiline_version='1.2.3
junk'
if "$script_dir/validate-release-version.sh" "$multiline_version" >/dev/null 2>&1; then
  echo "multiline version passed version policy" >&2
  exit 1
fi
if "$script_dir/validate-release-version.sh" --tag "v$multiline_version" >/dev/null 2>&1; then
  echo "multiline tag passed version policy" >&2
  exit 1
fi
"$script_dir/validate-release-version.sh" 1.2.3-alpha-1
"$script_dir/validate-release-version.sh" 1.2.3-0
if PATH="$temp:$PATH" GITHUB_REF_TYPE=tag GITHUB_REF_NAME=vinvalid \
  "$script_dir/release-preflight.sh" invalid image ghcr.io/example/bundles/order-review:invalid chart >/dev/null 2>&1; then
  echo "malformed publish version passed preflight" >&2
  exit 1
fi
# Recovery invokes the same validator rather than keeping a second regex.
grep -Fq 'validate-release-version.sh --tag' "$script_dir/../workflows/recover-release.yml"
grep -Fq 'TAG' "$script_dir/../workflows/recover-release.yml"

if PATH="$temp:$PATH" GITHUB_REF_TYPE=branch GITHUB_REF_NAME=main \
  "$script_dir/release-preflight.sh" "$version" image "$bundle" chart >/dev/null 2>&1; then
  echo "untagged source passed preflight" >&2
  exit 1
fi

if PATH="$temp:$PATH" GITHUB_REF_TYPE=tag GITHUB_REF_NAME=v9.9.9 \
  "$script_dir/release-preflight.sh" 9.9.9 image ghcr.io/example/bundles/order-review:9.9.9 chart >/dev/null 2>&1; then
  echo "mismatched first-party versions passed preflight" >&2
  exit 1
fi

legacy_bundle="ghcr.io/example/bundles/flow-ui""-demo:$version"
if PATH="$temp:$PATH" GITHUB_REF_TYPE=tag GITHUB_REF_NAME="v$version" \
  "$script_dir/release-preflight.sh" "$version" image "$legacy_bundle" chart >/dev/null 2>&1; then
  echo "legacy bundle coordinate passed preflight" >&2
  exit 1
fi

for malformed_bundle in \
  "ghcr.io//bundles/order-review:$version" \
  "ghcr.io/a/b/bundles/order-review:$version" \
  "ghcr.io/UPPER/bundles/order-review:$version"; do
  if PATH="$temp:$PATH" GITHUB_REF_TYPE=tag GITHUB_REF_NAME="v$version" \
    "$script_dir/release-preflight.sh" "$version" image "$malformed_bundle" chart >/dev/null 2>&1; then
    echo "malformed bundle coordinate passed preflight: $malformed_bundle" >&2
    exit 1
  fi
done

PATH="$temp:$PATH" GITHUB_REF_TYPE=tag GITHUB_REF_NAME="v$version" \
  "$script_dir/release-preflight.sh" "$version" image "$bundle" chart

cat >"$temp/crane" <<'EOF'
#!/bin/sh
echo sha256:existing
EOF
chmod +x "$temp/crane"
if PATH="$temp:$PATH" GITHUB_REF_TYPE=tag GITHUB_REF_NAME="v$version" \
  "$script_dir/release-preflight.sh" "$version" image "$bundle" chart >/dev/null 2>&1; then
  echo "existing version passed preflight" >&2
  exit 1
fi
