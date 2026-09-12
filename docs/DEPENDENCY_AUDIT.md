# Dependency Vulnerability Audit

CI runs `govulncheck` for every discovered Go module and `npm audit` for the
VS Code extension, including development dependencies, at moderate severity or above.
It scans the production image and the standalone order-review
business-executor image with Trivy at high and critical severity.

Reproduce the checks from the repository root:

```bash
set -eu
export GOTOOLCHAIN=go1.25.13
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

The [2026-09-12 repository audit](audits/repository-state-2026-09-12.md#dependency-pr-disposition)
records the consolidated Go updates and the remaining toolchain and extension PRs.
