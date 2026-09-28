#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
mkdir -p "$temp/mock" "$temp/state"
export MOCK_STATE="$temp/state"
export IMAGE_REPO=ghcr.io/example/effectusd
export BUNDLE_REPO=ghcr.io/example/bundles/order-review
export CHART_REPO=ghcr.io/example/helm/effectusd
export CHART_STAGE_REPO=ghcr.io/example/helm-staging-source/effectusd
export SOURCE_SHA=source
export VERSION=1.2.3
IMAGE_DIGEST="sha256:$(printf '%064d' 0)"
BUNDLE_DIGEST="sha256:$(printf '%064d' 1)"
CHART_DIGEST="sha256:$(printf '%064d' 2)"
bad_digest="sha256:$(printf '%064d' 3)"
AMD64_DIGEST="sha256:$(printf '%064d' 4)"
ARM64_DIGEST="sha256:$(printf '%064d' 5)"
export IMAGE_DIGEST BUNDLE_DIGEST CHART_DIGEST AMD64_DIGEST ARM64_DIGEST

cat > "$temp/mock/crane" <<'EOF'
#!/bin/sh
set -eu
case "$1" in
  digest)
    if [ "$2" = --platform ]; then
      test "$4" = "$IMAGE_REPO@$IMAGE_DIGEST"
      case "$3" in
        linux/amd64) echo "$AMD64_DIGEST" ;;
        linux/arm64) echo "$ARM64_DIGEST" ;;
        *) exit 2 ;;
      esac
      exit 0
    fi
    ref=$2
    if [ "${MOCK_REGISTRY_ERROR:-}" = "$ref" ]; then
      echo 'registry unavailable' >&2
      exit 1
    fi
    case "$ref" in
      "$IMAGE_REPO:staging-$SOURCE_SHA") echo "${MOCK_IMAGE_STAGE_DIGEST:-$IMAGE_DIGEST}" ;;
      "$BUNDLE_REPO:staging-$SOURCE_SHA") echo "$BUNDLE_DIGEST" ;;
      "$CHART_STAGE_REPO:$VERSION") echo "$CHART_DIGEST" ;;
      "$IMAGE_REPO:$VERSION") file=$MOCK_STATE/image ;;
      "$BUNDLE_REPO:$VERSION") file=$MOCK_STATE/bundle ;;
      "$CHART_REPO:$VERSION") file=$MOCK_STATE/chart ;;
      *) echo "unexpected digest ref: $ref" >&2; exit 2 ;;
    esac
    if [ "${file:-}" ]; then
      if [ -f "$file" ]; then cat "$file"; else echo 'MANIFEST_UNKNOWN' >&2; exit 1; fi
    fi
    ;;
  tag)
    printf 'tag %s %s\n' "$2" "$3" >> "$MOCK_STATE/writes"
    case "$2" in
      "$IMAGE_REPO@$IMAGE_DIGEST") printf '%s\n' "${MOCK_BAD_WRITE:-$IMAGE_DIGEST}" > "$MOCK_STATE/image" ;;
      "$BUNDLE_REPO@$BUNDLE_DIGEST") printf '%s\n' "$BUNDLE_DIGEST" > "$MOCK_STATE/bundle" ;;
      *) exit 2 ;;
    esac
    ;;
  *) exit 2 ;;
esac
EOF
cat > "$temp/mock/cosign" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$MOCK_STATE/verifications"
if [ "${MOCK_COSIGN_FAIL:-}" = "$1" ]; then exit 1; fi
if [ "${MOCK_INDEX_SBOM_MISSING:-}" = 1 ] &&
  [ "$1" = verify-attestation ] &&
  [ "$3" = cyclonedx ] &&
  [ "$4" = "$IMAGE_REPO@$IMAGE_DIGEST" ]; then
  exit 1
fi
EOF
cat > "$temp/mock/oras" <<'EOF'
#!/bin/sh
set -eu
printf 'copy %s %s\n' "$3" "$4" >> "$MOCK_STATE/writes"
test "$1" = copy && test "$2" = --recursive
test "$3" = "$CHART_STAGE_REPO@$CHART_DIGEST"
test "$4" = "$CHART_REPO:$VERSION"
printf '%s\n' "$CHART_DIGEST" > "$MOCK_STATE/chart"
EOF
chmod +x "$temp/mock/"*

run_helper() {
  PATH="$temp/mock:$PATH" "$script_dir/recover-promote.sh" promote \
    "$VERSION" "$SOURCE_SHA" "$IMAGE_REPO" "$BUNDLE_REPO" \
    "$CHART_REPO" "$CHART_STAGE_REPO" \
    'https://github.com/example/repo/.github/workflows/publish.yml@refs/tags/v1.2.3' \
    "$temp/outputs" "$IMAGE_DIGEST" "$BUNDLE_DIGEST" "$CHART_DIGEST"
}
verify_helper() {
  PATH="$temp/mock:$PATH" "$script_dir/recover-promote.sh" verify \
    "$VERSION" "$SOURCE_SHA" "$IMAGE_REPO" "$BUNDLE_REPO" \
    "$CHART_REPO" "$CHART_STAGE_REPO" \
    'https://github.com/example/repo/.github/workflows/publish.yml@refs/tags/v1.2.3' \
    "$temp/outputs"
}
reset_state() {
  rm -f "$MOCK_STATE"/* "$temp/outputs"
}
assert_no_writes() {
  if [ -s "$MOCK_STATE/writes" ]; then
    echo 'recovery mutated a tag when it should have stopped' >&2
    exit 1
  fi
}

# Validation leaves all final tags absent and returns the exact digest to scan.
verify_helper
assert_no_writes
test "$(awk -F= '$1 == "image_digest" {print $2}' "$temp/outputs")" = "$IMAGE_DIGEST"
reset_state

# All missing tags are promoted only after all three signed stages are checked.
run_helper
test "$(wc -l < "$MOCK_STATE/writes")" -eq 3
test "$(wc -l < "$MOCK_STATE/verifications")" -eq 9
test "$(awk -F= '$1 == "image_digest" {print $2}' "$temp/outputs")" = "$IMAGE_DIGEST"

# New publishers attest both platform digests instead of the index.
reset_state
export MOCK_INDEX_SBOM_MISSING=1
run_helper
grep -Fq "$IMAGE_REPO@$AMD64_DIGEST" "$MOCK_STATE/verifications"
grep -Fq "$IMAGE_REPO@$ARM64_DIGEST" "$MOCK_STATE/verifications"
unset MOCK_INDEX_SBOM_MISSING

# A staging tag changed after the scan cannot be promoted.
reset_state
export MOCK_IMAGE_STAGE_DIGEST="$bad_digest"
if run_helper >/dev/null 2>&1; then
  echo 'changed staging image digest was promoted after scan' >&2
  exit 1
fi
assert_no_writes
unset MOCK_IMAGE_STAGE_DIGEST

# Re-running with matching tags is idempotent.
run_helper
rm -f "$MOCK_STATE/writes" "$temp/outputs"
run_helper
assert_no_writes

# A missing middle tag is the only tag repaired.
rm -f "$MOCK_STATE/bundle" "$MOCK_STATE/writes" "$temp/outputs"
run_helper
test "$(wc -l < "$MOCK_STATE/writes")" -eq 1
grep -Fq "tag $BUNDLE_REPO@$BUNDLE_DIGEST $VERSION" "$MOCK_STATE/writes"

# An existing mismatched last tag stops all writes, including earlier missing tags.
reset_state
printf '%s\n' "$bad_digest" > "$MOCK_STATE/chart"
if run_helper >/dev/null 2>&1; then
  echo 'mismatched chart digest was accepted' >&2
  exit 1
fi
assert_no_writes

# Registry outages and invalid original signatures cannot be treated as absence.
reset_state
export MOCK_REGISTRY_ERROR="$BUNDLE_REPO:$VERSION"
if run_helper >/dev/null 2>&1; then
  echo 'registry failure was accepted as a missing tag' >&2
  exit 1
fi
assert_no_writes
unset MOCK_REGISTRY_ERROR

reset_state
export MOCK_COSIGN_FAIL=verify-attestation
if run_helper >/dev/null 2>&1; then
  echo 'unsigned staging evidence was accepted' >&2
  exit 1
fi
assert_no_writes
unset MOCK_COSIGN_FAIL

# A write that does not produce the expected digest stops recovery.
reset_state
export MOCK_BAD_WRITE="$bad_digest"
if run_helper >/dev/null 2>&1; then
  echo 'wrong digest after promotion was accepted' >&2
  exit 1
fi
test "$(wc -l < "$MOCK_STATE/writes")" -eq 1
