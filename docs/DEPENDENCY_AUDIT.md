# Dependency Vulnerability Audit

CI runs `govulncheck` for every discovered Go module and `npm audit` for the
VS Code extension, including development dependencies, at moderate severity or above.
It scans the production image and the standalone order-review
business-executor image with Trivy at high and critical severity.

Reproduce the checks from the repository root:

```bash
set -eu
export GOTOOLCHAIN=go1.26.8
go install golang.org/x/vuln/cmd/govulncheck@v1.7.0
go run ./internal/guardrails/cmd modules > /tmp/effectus-audit-modules.txt
while IFS= read -r module; do
  (cd "$module" && govulncheck ./...) || exit
done < /tmp/effectus-audit-modules.txt
(cd tools/vscode-extension && npm ci && npm audit --audit-level=moderate)
docker build -t effectus:audit .
docker build --file examples/standalone_executor/Dockerfile \
  --tag effectus/business-executor:audit .
```

Test-only service stacks are in `tests/fixtures`. They are not examples or
production deployment templates.

## Toolchain and extension maintenance

Goose 3.28.0 requires Go 1.26. The root module declares `go 1.26.0` and pins
`toolchain go1.26.8`; both Docker builders use the same patched Go release and
immutable image digest. CI uses golangci-lint 2.13.2, whose official binary was
built with Go 1.27 and can analyze the Go 1.26 module. This coordinated update
supersedes [PR #69](https://github.com/josephjohncox/effectus/pull/69), which
changed the module requirement without updating the build environment.

The VS Code extension is declarative: its manifest loads language configuration,
grammar, snippets, and icons without an extension-host entry point.
Removing the empty activation module also removes the direct TypeScript,
Node/editor type packages, TypeScript ESLint integration, and Mocha dependencies.
The superseded updates are [#68](https://github.com/josephjohncox/effectus/pull/68),
[#71](https://github.com/josephjohncox/effectus/pull/71), and
[#72](https://github.com/josephjohncox/effectus/pull/72).
The editor requirement stays `^1.74.0`; development and CI use Node 22 or later.
Node's built-in test runner validates the manifest and referenced assets, and
VSIX packaging runs those tests. ESLint and the VSIX packager remain development
dependencies and remain covered by the npm vulnerability gate.

The [2026-09-12 repository audit](audits/repository-state-2026-09-12.md#dependency-pr-disposition)
records the branch and PR state before this maintenance.
