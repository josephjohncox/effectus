#!/bin/sh
# shellcheck disable=SC2016 # Compare literal workflow expressions.
# Keep public compatibility and artifact verification ahead of completion.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
workflows=$script_dir/../workflows

for file in "$workflows/publish.yml" "$workflows/recover-release.yml"; do
  if ! awk '
    /^      - name: Scan .*AMD64 and ARM64 image artifacts$/ { scan = NR; scan_count++ }
    /^      - name: Verify public Go-proxy compatibility before completion$/ { smoke = NR; smoke_count++ }
    /^      - name: Create and sign .*completion manifest$/ { manifest = NR; manifest_count++ }
    /^      - name: Create GitHub release completion marker last$/ { release = NR; release_count++ }
    END {
      if (scan_count != 1 || smoke_count != 1 || manifest_count != 1 || release_count != 1 ||
          !(scan < smoke && smoke < manifest && manifest < release)) exit 1
    }
  ' "$file"; then
    echo "release completion order is unsafe: $file" >&2
    exit 1
  fi
  grep -Fq 'scripts/compat-proxy-smoke.sh "$VERSION"' "$file"
  grep -Fq 'scan-published-image.sh' "$file"
done

if ! awk '
  /^      - name: Scan the pushed AMD64 and ARM64 image artifacts$/ { scan = NR }
  /^      - name: Sign and verify staging references$/ { attest = NR }
  /^      - name: Promote immutable version references in order$/ { promote = NR }
  END { if (!(scan < attest && attest < promote)) exit 1 }
' "$workflows/publish.yml"; then
  echo 'publish promotes an image before scanning and attesting it' >&2
  exit 1
fi
if ! awk '
  /^      - name: Validate signed staging references and final destinations$/ { verify = NR }
  /^      - name: Scan recovered published AMD64 and ARM64 image artifacts$/ { scan = NR }
  /^      - name: Sign and verify recovered image attestations$/ { attest = NR }
  /^      - name: Sign and verify recovered asset checksums$/ { checksums = NR }
  /^      - name: Promote verified OCI digests to missing final tags$/ { promote = NR }
  /^      - name: Repair and verify final chart signatures$/ { chart = NR }
  END {
    if (!(verify < scan && scan < attest && attest < checksums &&
          checksums < promote && promote < chart)) exit 1
  }
' "$workflows/recover-release.yml"; then
  echo 'recovery promotes final tags before staging verification is complete' >&2
  exit 1
fi
grep -Fq 'recover-promote.sh' "$workflows/recover-release.yml"
grep -Fq '            verify ' "$workflows/recover-release.yml"
grep -Fq '            promote ' "$workflows/recover-release.yml"

grep -Fq 'group: release-${{ github.ref_name }}' "$workflows/publish.yml"
grep -Fq 'group: release-${{ inputs.tag }}' "$workflows/recover-release.yml"
grep -Fq 'image-amd64-sbom.cdx.json "${IMAGE_REPO}@${amd64_digest}"' "$workflows/publish.yml"
grep -Fq 'image-arm64-sbom.cdx.json "${IMAGE_REPO}@${arm64_digest}"' "$workflows/publish.yml"
grep -Fq '"${IMAGE_REPO}@${platform_digest}"' "$workflows/publish.yml"
grep -Fq '"dist/image-${arch}-sbom.cdx.json" "$platform_ref"' "$workflows/recover-release.yml"
if grep -Fq 'effectus:release' "$workflows/publish.yml" "$workflows/recover-release.yml"; then
  echo 'release workflow scans an unpushed local image' >&2
  exit 1
fi
