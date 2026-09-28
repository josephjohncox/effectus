#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
mkdir -p "$temp/mock" "$temp/dist" "$temp/.docker"
export DOCKER_CONFIG="$temp/.docker"
export MOCK_LOG="$temp/calls"
export IMAGE_REPO=ghcr.io/example/effectusd
INDEX_DIGEST="sha256:$(printf '%064d' 0)"
AMD64_DIGEST="sha256:$(printf '%064d' 1)"
ARM64_DIGEST="sha256:$(printf '%064d' 2)"
export INDEX_DIGEST AMD64_DIGEST ARM64_DIGEST

cat > "$temp/mock/crane" <<'EOF'
#!/bin/sh
set -eu
case "$1" in
  digest)
    if [ "$2" = --platform ]; then
      test "$4" = "$IMAGE_REPO@$INDEX_DIGEST"
      case "$3" in
        linux/amd64) echo "$AMD64_DIGEST" ;;
        linux/arm64) echo "${MOCK_MISSING_ARM:-$ARM64_DIGEST}" ;;
        *) exit 2 ;;
      esac
    else
      test "$2" = "$IMAGE_REPO@$INDEX_DIGEST"
      echo "$INDEX_DIGEST"
    fi
    ;;
  config)
    case "$2" in
      "$IMAGE_REPO@$AMD64_DIGEST") printf '{"os":"linux","architecture":"amd64"}\n' ;;
      "$IMAGE_REPO@$ARM64_DIGEST")
        printf '{"os":"linux","architecture":"%s"}\n' "${MOCK_BAD_ARCH:-arm64}" ;;
      *) exit 2 ;;
    esac
    ;;
  *) exit 2 ;;
esac
EOF
cat > "$temp/mock/docker" <<'EOF'
#!/bin/sh
set -eu
printf 'docker %s\n' "$*" >> "$MOCK_LOG"
case "$*" in
  *"-v $DOCKER_CONFIG:/root/.docker:ro"*"-e DOCKER_CONFIG=/root/.docker"*"--image-src remote"*) ;;
  *) echo 'registry scanner lacks Docker credentials or remote source' >&2; exit 1 ;;
esac
if [ "${MOCK_SCAN_FAIL:-}" = arm64 ]; then
  case "$*" in *"$ARM64_DIGEST"*) exit 1 ;; esac
fi
EOF
cat > "$temp/mock/syft" <<'EOF'
#!/bin/sh
set -eu
printf 'syft %s\n' "$*" >> "$MOCK_LOG"
test "$1" = --from && test "$2" = registry
case "$3" in
  "$IMAGE_REPO@$AMD64_DIGEST"|"$IMAGE_REPO@$ARM64_DIGEST") ;;
  *) echo 'SBOM source was not a published platform digest' >&2; exit 1 ;;
esac
test "$4" = -o
case "$5" in
  cyclonedx-json=*) file=${5#cyclonedx-json=} ;;
  *) exit 2 ;;
esac
printf '{"source":"%s"}\n' "$3" > "$file"
EOF
chmod +x "$temp/mock/"*

run_scan() {
  PATH="$temp/mock:$PATH" "$script_dir/scan-published-image.sh" \
    "$IMAGE_REPO" "$INDEX_DIGEST" "$temp/dist"
}

run_scan
test "$(grep -c '^docker ' "$MOCK_LOG")" -eq 2
test "$(grep -c '^syft ' "$MOCK_LOG")" -eq 2
jq -e --arg index "$INDEX_DIGEST" --arg amd64 "$AMD64_DIGEST" \
  --arg arm64 "$ARM64_DIGEST" \
  '.index == $index and .platforms["linux/amd64"] == $amd64 and .platforms["linux/arm64"] == $arm64' \
  "$temp/dist/image-platforms.json" >/dev/null
jq -e --arg ref "$IMAGE_REPO@$AMD64_DIGEST" '.source == $ref' \
  "$temp/dist/image-amd64-sbom.cdx.json" >/dev/null
jq -e --arg ref "$IMAGE_REPO@$ARM64_DIGEST" '.source == $ref' \
  "$temp/dist/image-arm64-sbom.cdx.json" >/dev/null

# A missing platform, wrong manifest config, or failed scan stops SBOM issuance.
export MOCK_MISSING_ARM="$INDEX_DIGEST"
if run_scan >/dev/null 2>&1; then
  echo 'missing ARM64 platform passed image scan' >&2
  exit 1
fi
unset MOCK_MISSING_ARM

export MOCK_BAD_ARCH=amd64
if run_scan >/dev/null 2>&1; then
  echo 'wrong ARM64 config passed image scan' >&2
  exit 1
fi
unset MOCK_BAD_ARCH

export MOCK_SCAN_FAIL=arm64
if run_scan >/dev/null 2>&1; then
  echo 'failed ARM64 vulnerability scan passed' >&2
  exit 1
fi
