#!/bin/sh
# Scan and describe both platform manifests in the exact pushed OCI index.
set -eu

if [ "$#" -ne 3 ]; then
  echo "usage: scan-published-image.sh IMAGE_REPO INDEX_DIGEST OUTPUT_DIR" >&2
  exit 2
fi
image_repo=$1
index_digest=$2
output_dir=$3
trivy='aquasec/trivy:0.69.3@sha256:bcc376de8d77cfe086a917230e818dc9f8528e3c852f7b1aff648949b6258d1c'
docker_config_dir=${DOCKER_CONFIG:-"${HOME}/.docker"}

if ! printf '%s\n' "$index_digest" | grep -Eq '^sha256:[0-9a-f]{64}$'; then
  echo "invalid image index digest: $index_digest" >&2
  exit 2
fi
test -d "$output_dir"
test "$(crane digest "${image_repo}@${index_digest}")" = "$index_digest"

for arch in amd64 arm64; do
  platform_digest=$(crane digest --platform "linux/$arch" "${image_repo}@${index_digest}")
  if ! printf '%s\n' "$platform_digest" | grep -Eq '^sha256:[0-9a-f]{64}$' ||
    [ "$platform_digest" = "$index_digest" ]; then
    echo "missing linux/$arch manifest in pushed image index" >&2
    exit 1
  fi
  crane config "${image_repo}@${platform_digest}" |
    jq -e --arg arch "$arch" '.os == "linux" and .architecture == $arch' >/dev/null

  # Force registry access. The runner's locally built image is not the
  # multi-platform artifact that Buildx actually pushed.
  docker run --rm \
    -v "${docker_config_dir}:/root/.docker:ro" \
    -e DOCKER_CONFIG=/root/.docker \
    "$trivy" image --image-src remote --scanners vuln \
    --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 \
    "${image_repo}@${platform_digest}"
  syft --from registry "${image_repo}@${platform_digest}" \
    -o "cyclonedx-json=${output_dir}/image-${arch}-sbom.cdx.json"
  if [ "$arch" = amd64 ]; then
    amd64_digest=$platform_digest
  else
    arm64_digest=$platform_digest
  fi
done

jq -n --arg index "$index_digest" \
  --arg amd64 "$amd64_digest" --arg arm64 "$arm64_digest" \
  '{index:$index,platforms:{"linux/amd64":$amd64,"linux/arm64":$arm64}}' \
  > "${output_dir}/image-platforms.json"
