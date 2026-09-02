# Dependency Vulnerability Audit

CI runs `govulncheck` for every discovered Go module and `npm audit` for the
VS Code extension. It scans the production image and the standalone order-review
business-executor image with Trivy at high and critical severity.

Reproduce the checks from the repository root:

```bash
govulncheck ./...
(cd tools/vscode-extension && npm ci && npm audit --omit=dev --audit-level=high)
docker build -t effectus:audit .
docker build --file examples/standalone_executor/Dockerfile \
  --tag effectus/business-executor:audit .
```

Test-only service stacks are in `tests/fixtures`. They are not examples or
production deployment templates.
