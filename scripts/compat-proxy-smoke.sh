#!/bin/sh
# Verify that a released root module exposes the frozen v0.3 compatibility paths.
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: compat-proxy-smoke.sh ROOT_VERSION" >&2
  exit 2
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
version=${1#v}
if ! "$script_dir/../.github/scripts/validate-release-version.sh" "$version" >/dev/null 2>&1; then
  echo "root version is not semantic: $1" >&2
  exit 2
fi

# Production always resolves through the public Go proxy. The test-only
# override supports the hermetic file:// proxy fixture in this repository.
compat_proxy=${EFFECTUS_COMPAT_TEST_GOPROXY:-https://proxy.golang.org}
case "$compat_proxy" in
  ""|*','*|*' '*|*'\t'*)
    echo "compatibility proxy must be one proxy URL" >&2
    exit 2
    ;;
esac
if [ -n "${EFFECTUS_COMPAT_TEST_GOPROXY:-}" ]; then
  compat_sumdb=off
else
  compat_sumdb=sum.golang.org
fi

temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT HUP INT TERM
cd "$temp"
go mod init example.com/effectus-compat-v03-smoke >/dev/null
gen_test_file=compat_test.go
cat >"$gen_test_file" <<'EOF'
package smoke

import (
	"context"
	"testing"

	"github.com/josephjohncox/effectus/compat/v03/embedded"
	"github.com/josephjohncox/effectus/compat/v03/executorhttp"
	"github.com/josephjohncox/effectus/compat/v03/invocation"
)

func TestV03CompatibilitySurfaceCompiles(t *testing.T) {
	var _ embedded.HandlerFunc = func(context.Context, invocation.Request) invocation.Outcome {
		return embedded.Success(nil)
	}
	_, err := executorhttp.NewHandler(executorhttp.Options{}, func(context.Context, executorhttp.Request) executorhttp.Outcome {
		return executorhttp.Success(map[string]any{"ok": true})
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = invocation.NewHTTPExecutor(invocation.HTTPExecutor{URL: "https://executor.example"})
	if err != nil {
		t.Fatal(err)
	}
}
EOF

# Do not permit direct VCS fallback: GOPROXY names exactly one proxy, and all
# private-module and proxy-bypass settings are cleared for every Go command.
GOPROXY="$compat_proxy" GOPRIVATE='' GONOPROXY='' GONOSUMDB='' GOSUMDB="$compat_sumdb" \
  go mod edit -require="github.com/josephjohncox/effectus@v${version}"
GOPROXY="$compat_proxy" GOPRIVATE='' GONOPROXY='' GONOSUMDB='' GOSUMDB="$compat_sumdb" go mod tidy
GOPROXY="$compat_proxy" GOPRIVATE='' GONOPROXY='' GONOSUMDB='' GOSUMDB="$compat_sumdb" go test ./...
