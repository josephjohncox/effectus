#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
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

if PATH="$temp:$PATH" GITHUB_REF_TYPE=branch GITHUB_REF_NAME=main \
  "$script_dir/release-preflight.sh" "$version" image bundle chart >/dev/null 2>&1; then
  echo "untagged source passed preflight" >&2
  exit 1
fi

if PATH="$temp:$PATH" GITHUB_REF_TYPE=tag GITHUB_REF_NAME=v9.9.9 \
  "$script_dir/release-preflight.sh" 9.9.9 image bundle chart >/dev/null 2>&1; then
  echo "mismatched first-party versions passed preflight" >&2
  exit 1
fi

PATH="$temp:$PATH" GITHUB_REF_TYPE=tag GITHUB_REF_NAME="v$version" \
  "$script_dir/release-preflight.sh" "$version" image bundle chart

cat >"$temp/crane" <<'EOF'
#!/bin/sh
echo sha256:existing
EOF
chmod +x "$temp/crane"
if PATH="$temp:$PATH" GITHUB_REF_TYPE=tag GITHUB_REF_NAME="v$version" \
  "$script_dir/release-preflight.sh" "$version" image bundle chart >/dev/null 2>&1; then
  echo "existing version passed preflight" >&2
  exit 1
fi
