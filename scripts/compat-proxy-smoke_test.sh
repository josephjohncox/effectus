#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
temp=$(mktemp -d)
trap 'chmod -R u+w "$temp"; rm -rf "$temp"' EXIT HUP INT TERM
module=github.com/josephjohncox/effectus

if "$script_dir/compat-proxy-smoke.sh" not-a-version >/dev/null 2>&1; then
  echo "invalid root version passed compatibility smoke validation" >&2
  exit 1
fi

if ! grep -Fq 'https://proxy.golang.org' "$script_dir/compat-proxy-smoke.sh"; then
  echo "compatibility smoke does not default strictly to proxy.golang.org" >&2
  exit 1
fi
if grep -Eq 'GOPROXY=.*direct|GOPROXY=.*[,|]' "$script_dir/compat-proxy-smoke.sh"; then
  echo "compatibility smoke permits a direct VCS fallback" >&2
  exit 1
fi

make_proxy() {
  proxy=$1
  version=$2
  absent=${3:-}
  module_dir="${module}@v${version}"
  source="$proxy/source"
  root="$source/$module_dir"
  version_dir="$proxy/$module/@v"

  mkdir -p "$root" "$version_dir"
  printf 'module %s\n\ngo 1.25.0\n' "$module" >"$root/go.mod"
  printf '{"Version":"v%s","Time":"2020-01-01T00:00:00Z"}\n' "$version" \
    >"$version_dir/v$version.info"
  cp "$root/go.mod" "$version_dir/v$version.mod"

  if [ "$absent" != "invocation" ]; then
    mkdir -p "$root/compat/v03/invocation"
    cat >"$root/compat/v03/invocation/compat.go" <<'GOEOF'
package invocation

type Request struct{}
type Outcome struct{}
type HTTPExecutor struct{ URL string }

func NewHTTPExecutor(HTTPExecutor) (any, error) { return nil, nil }
GOEOF
  fi

  if [ "$absent" != "embedded" ]; then
    mkdir -p "$root/compat/v03/embedded"
    cat >"$root/compat/v03/embedded/compat.go" <<'GOEOF'
package embedded

import (
	"context"

	"github.com/josephjohncox/effectus/compat/v03/invocation"
)

type HandlerFunc func(context.Context, invocation.Request) invocation.Outcome

func Success(any) invocation.Outcome { return invocation.Outcome{} }
GOEOF
  fi

  if [ "$absent" != "executorhttp" ]; then
    mkdir -p "$root/compat/v03/executorhttp"
    cat >"$root/compat/v03/executorhttp/compat.go" <<'GOEOF'
package executorhttp

import (
	"context"

	"github.com/josephjohncox/effectus/compat/v03/invocation"
)

type Request = invocation.Request
type Outcome = invocation.Outcome
type Options struct{}

func NewHandler(Options, func(context.Context, Request) Outcome) (any, error) {
	return nil, nil
}

func Success(map[string]any) Outcome { return invocation.Outcome{} }
GOEOF
  fi

  (
    cd "$source"
    zip -q -r "$version_dir/v$version.zip" "$module_dir"
  )
}

run_smoke() {
  proxy=$1
  version=$2
  cache_name=$3
  EFFECTUS_COMPAT_TEST_GOPROXY="file://$proxy" \
    GOMODCACHE="$temp/modcache-$cache_name" \
    GOCACHE="$temp/gocache-$cache_name" \
    "$script_dir/compat-proxy-smoke.sh" "v$version"
}

# Valid SemVer boundary forms must reach a real Go command through the proxy.
for version in 0.0.0 1.2.3 1.2.3-0 1.2.3-alpha 1.2.3-alpha.1 1.2.3-rc.1 1.2.3-alpha-1; do
  proxy="$temp/proxy-$version"
  make_proxy "$proxy" "$version"
  run_smoke "$proxy" "$version" "$version"
done

# SemVer core and numeric prerelease identifiers cannot contain leading zeroes.
for version in 01.2.3 1.02.3 1.2.03 1.2.3-01 1.2.3-alpha.01; do
  if "$script_dir/compat-proxy-smoke.sh" "v$version" >/dev/null 2>&1; then
    echo "invalid semantic version passed compatibility smoke validation: $version" >&2
    exit 1
  fi
done

version=0.0.0-compat-smoke
full_proxy="$temp/full-proxy"
make_proxy "$full_proxy" "$version"
run_smoke "$full_proxy" "$version" full

for absent in embedded executorhttp invocation; do
  proxy="$temp/proxy-without-$absent"
  make_proxy "$proxy" "$version" "$absent"
  if run_smoke "$proxy" "$version" "$absent" >/dev/null 2>&1; then
    echo "compatibility smoke passed without compat/v03/$absent" >&2
    exit 1
  fi
done
