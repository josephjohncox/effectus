# Dependency Vulnerability Audit

Audit date: 2026-08-31

This report records the scans used for the v0.3.0 release.
It also records findings that upstream projects have not resolved.

## Go modules

The audit used `govulncheck v1.3.0` with the current vulnerability database.

```bash
govulncheck ./...
```

The root module and all example modules report no called vulnerabilities.
The update includes patched gRPC, pgx, AWS EventStream, AWS S3, `x/net`, `klauspost/compress`, and `edwards25519` versions.
The Go toolchain is pinned to Go 1.25.13.

Dependabot alerts describe repository dependency state. They do not replace the reachable-code result from `govulncheck`.

## VS Code extension

The production dependency audit reports zero vulnerabilities.
The full development dependency audit reports two low-severity findings in `diff`, through Mocha. No nonbreaking upstream update is available. These packages do not ship in the VSIX runtime dependency set.

The extension no longer uses Axios or protobufjs.
The runtime HTTP client now uses bounded Node.js HTTP requests.
The extension uses the maintained `@vscode/vsce` package.
The lock file pins the complete npm dependency graph.

## Containers

The audit used Trivy 0.69.3 with high and critical severities.
Each custom image was built before its scan.

These images report no high or critical findings:

- The production `effectusd` image.
- The fraud mock image.
- The patched PostgreSQL and wal2json image.
- The patched MySQL image.
- The patched RabbitMQ image.
- The Redis 7.4 Alpine image.
- The patched MinIO client image.
- The standalone business-executor image.

The custom images use pinned base-image digests.
The builds also pin source commits for gosu, wal2json, MinIO, and the MinIO client.

### Residual upstream findings

The patched MinIO server still reports six high or critical application findings.
The current MinIO source has no fixed release or commit for these findings.
The findings include OIDC, LDAP, metadata, memory, and unauthenticated-write issues.
Do not expose the warehouse development MinIO service to an untrusted network.

Trino 483 reports four Java findings and ten launcher findings.
Trino 483 is the newest tested image at this audit date.
The fixed Java and Go versions are not available in a published Trino image.
The warehouse stack binds Trino for local development only.
Do not use this image as a production baseline.

The scans include unfixed findings in their reports.
CI can fail on fixed high or critical findings with `--ignore-unfixed --exit-code 1`.
This setting does not mean that unfixed findings are safe.

## GitHub Actions

All third-party actions use full commit SHAs.
The workflow grants read-only repository contents by default.
CI now runs Go vulnerability scans, example-module builds, npm audit, TypeScript compilation, lint checks, and fixed-vulnerability scans for the production and custom example images.

## Reproduction

Run these commands from the repository root:

```bash
govulncheck ./...
(cd examples && govulncheck ./...)
(cd tools/vscode-extension && npm ci && npm audit --omit=dev --audit-level=high)
(cd tools/vscode-extension && npm audit --audit-level=high)
docker build -t effectus:audit .
for spec in \
  fraud-mocks:examples/fraud_e2e/mocks \
  postgres-wal2json:examples/cdc_stack/postgres \
  mysql:examples/cdc_stack/mysql \
  rabbitmq:examples/cdc_stack/rabbitmq \
  minio:examples/warehouse_sources/devstack/minio \
  minio-mc:examples/warehouse_sources/devstack/minio-mc; do
  name="${spec%%:*}"
  context="${spec#*:}"
  docker build -t "effectus/${name}:audit" "$context"
done
docker build \
  --file examples/standalone_executor/Dockerfile \
  --tag effectus/business-executor:audit .
for image in \
  effectus:audit \
  effectus/fraud-mocks:audit \
  effectus/postgres-wal2json:audit \
  effectus/mysql:audit \
  effectus/rabbitmq:audit \
  effectus/minio:audit \
  effectus/minio-mc:audit \
  effectus/business-executor:audit; do
  docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
    aquasec/trivy:0.69.3@sha256:bcc376de8d77cfe086a917230e818dc9f8528e3c852f7b1aff648949b6258d1c \
    image --scanners vuln --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 "$image"
done

# Run a second report without --ignore-unfixed and retain its residual findings.
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
  aquasec/trivy:0.69.3@sha256:bcc376de8d77cfe086a917230e818dc9f8528e3c852f7b1aff648949b6258d1c \
  image --scanners vuln --severity HIGH,CRITICAL effectus/minio:audit
```
