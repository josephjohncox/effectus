#!/bin/sh
# Verify original staging evidence; promote only the exact digests already scanned.
set -eu

mode=${1:-}
if [ "$mode" != verify ] && [ "$mode" != promote ]; then
  echo "usage: recover-promote.sh verify|promote VERSION SOURCE_SHA IMAGE_REPO BUNDLE_REPO CHART_REPO CHART_STAGE_REPO ORIGINAL_IDENTITY OUTPUT_FILE [EXPECTED_IMAGE_DIGEST EXPECTED_BUNDLE_DIGEST EXPECTED_CHART_DIGEST]" >&2
  exit 2
fi
shift
if { [ "$mode" = verify ] && [ "$#" -ne 8 ]; } ||
  { [ "$mode" = promote ] && [ "$#" -ne 11 ]; }; then
  echo "usage: recover-promote.sh verify|promote VERSION SOURCE_SHA IMAGE_REPO BUNDLE_REPO CHART_REPO CHART_STAGE_REPO ORIGINAL_IDENTITY OUTPUT_FILE [EXPECTED_IMAGE_DIGEST EXPECTED_BUNDLE_DIGEST EXPECTED_CHART_DIGEST]" >&2
  exit 2
fi
version=$1
source_sha=$2
image_repo=$3
bundle_repo=$4
chart_repo=$5
chart_stage_repo=$6
original_identity=$7
output_file=$8
issuer=https://token.actions.githubusercontent.com

image_digest=$(crane digest "${image_repo}:staging-${source_sha}")
bundle_digest=$(crane digest "${bundle_repo}:staging-${source_sha}")
chart_digest=$(crane digest "${chart_stage_repo}:${version}")

for digest in "$image_digest" "$bundle_digest" "$chart_digest"; do
  if ! printf '%s\n' "$digest" | grep -Eq '^sha256:[0-9a-f]{64}$'; then
    echo "staging reference returned an invalid digest: $digest" >&2
    exit 1
  fi
done

if [ "$mode" = promote ] &&
  { [ "$image_digest" != "$9" ] || [ "$bundle_digest" != "${10}" ] ||
    [ "$chart_digest" != "${11}" ]; }; then
  echo "staging digest changed after verification and image scan" >&2
  exit 1
fi

# A missing final tag is recoverable only when every staging artifact has the
# expected original publisher identity and provenance attestation.
for ref in \
  "${image_repo}@${image_digest}" \
  "${bundle_repo}@${bundle_digest}" \
  "${chart_stage_repo}@${chart_digest}"; do
  cosign verify "$ref" \
    --certificate-identity "$original_identity" \
    --certificate-oidc-issuer "$issuer" >/dev/null
  cosign verify-attestation --type slsaprovenance "$ref" \
    --certificate-identity "$original_identity" \
    --certificate-oidc-issuer "$issuer" >/dev/null
done

# Older releases attached one image SBOM to the index. New releases attach a
# separate SBOM to each exact platform manifest in the signed index.
image_ref="${image_repo}@${image_digest}"
if ! cosign verify-attestation --type cyclonedx "$image_ref" \
  --certificate-identity "$original_identity" \
  --certificate-oidc-issuer "$issuer" >/dev/null 2>&1; then
  for arch in amd64 arm64; do
    platform_digest=$(crane digest --platform "linux/$arch" "$image_ref")
    if ! printf '%s\n' "$platform_digest" | grep -Eq '^sha256:[0-9a-f]{64}$' ||
      [ "$platform_digest" = "$image_digest" ]; then
      echo "missing linux/$arch manifest in signed staging image" >&2
      exit 1
    fi
    cosign verify-attestation --type cyclonedx \
      "${image_repo}@${platform_digest}" \
      --certificate-identity "$original_identity" \
      --certificate-oidc-issuer "$issuer" >/dev/null
  done
fi
for ref in \
  "${bundle_repo}@${bundle_digest}" \
  "${chart_stage_repo}@${chart_digest}"; do
  cosign verify-attestation --type cyclonedx "$ref" \
    --certificate-identity "$original_identity" \
    --certificate-oidc-issuer "$issuer" >/dev/null
done

check_destination() {
  ref=$1
  expected=$2
  error_file=$(mktemp)
  if actual=$(crane digest "$ref" 2>"$error_file"); then
    rm -f "$error_file"
    if [ "$actual" != "$expected" ]; then
      echo "release destination has a different digest: $ref ($actual, expected $expected)" >&2
      exit 1
    fi
    printf 'present\n'
    return
  fi
  if ! grep -Eqi 'MANIFEST_UNKNOWN|NAME_UNKNOWN|manifest unknown|404 Not Found|status code 404' "$error_file"; then
    echo "cannot prove release destination is absent: $ref" >&2
    cat "$error_file" >&2
    rm -f "$error_file"
    exit 1
  fi
  rm -f "$error_file"
  printf 'missing\n'
}

# Resolve all final references before any registry write. A mismatch in the
# last destination must not cause the first missing tag to be promoted.
image_state=$(check_destination "${image_repo}:${version}" "$image_digest")
bundle_state=$(check_destination "${bundle_repo}:${version}" "$bundle_digest")
chart_state=$(check_destination "${chart_repo}:${version}" "$chart_digest")

if [ "$mode" = promote ] && [ "$image_state" = missing ]; then
  crane tag "${image_repo}@${image_digest}" "$version"
  test "$(crane digest "${image_repo}:${version}")" = "$image_digest"
fi
if [ "$mode" = promote ] && [ "$bundle_state" = missing ]; then
  crane tag "${bundle_repo}@${bundle_digest}" "$version"
  test "$(crane digest "${bundle_repo}:${version}")" = "$bundle_digest"
fi
if [ "$mode" = promote ] && [ "$chart_state" = missing ]; then
  oras copy --recursive "${chart_stage_repo}@${chart_digest}" \
    "${chart_repo}:${version}"
  test "$(crane digest "${chart_repo}:${version}")" = "$chart_digest"
fi

{
  printf 'image_digest=%s\n' "$image_digest"
  printf 'bundle_digest=%s\n' "$bundle_digest"
  printf 'chart_digest=%s\n' "$chart_digest"
} >> "$output_file"
