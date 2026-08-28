# effectusd Helm Chart

This chart deploys the `effectusd` runtime with an OCI bundle reference.

## Install

```bash
helm install effectusd oci://ghcr.io/OWNER/helm/effectusd \
  --version 1.0.0 \
  --set image.repository=ghcr.io/OWNER/effectusd \
  --set image.digest=sha256:IMAGE_DIGEST \
  --set bundle.ociRef=ghcr.io/OWNER/bundles/fraud-demo@sha256:BUNDLE_DIGEST \
  --set bundle.signatureVerifier=/usr/local/bin/effectus-verify-oci \
  --set postgres.existingSecret=effectusd-postgres \
  --set api.existingSecret=effectusd-api
```

## Configuration

Key values in `values.yaml`:

- `image.repository` plus an immutable `image.digest`
- `postgres.existingSecret` and `postgres.dsnKey` for the durable ledger
- `postgres.pool.*` for the runtime connection budget
- `migrations.existingSecret` for a separate DDL credential
- `bundle.ociRef` as a digest-pinned reference
- `bundle.signatureVerifier` for the fixed verifier executable
- `bundle.reloadInterval` must remain `0s` for the checked daemon
- `api.*` for auth, ACLs, rate limits, and trusted proxy CIDRs
- `podAnnotations` for an approved secret-reloader controller
- `facts.*` (store path, merge strategy, cache limits)
- `initContainers`, `extraVolumes`, `extraVolumeMounts` for the verifier or sidecar data
- `config.*` (mount a config map and pass `--config`)

### ConfigMap usage

```yaml
image:
  digest: "sha256:IMAGE_DIGEST"
postgres:
  existingSecret: "effectusd-postgres"
bundle:
  signatureVerifier: "/usr/local/bin/effectus-verify-oci"
api:
  authMode: "token"
  existingSecret: "effectusd-api"
config:
  enabled: true
  contents: |
    bundle:
      oci: "ghcr.io/OWNER/bundles/fraud-demo@sha256:BUNDLE_DIGEST"
    http:
      addr: ":8080"
    api:
      auth: "token"
```

Create `effectusd-api` as a Kubernetes Secret with the keys `api-token` and, optionally, `api-read-token`. Create `effectusd-postgres` with the `dsn` key. Do not put either secret in the ConfigMap.

The selected image or an extra read-only volume must provide the executable named by `bundle.signatureVerifier`. Startup fails if OCI content cannot be verified.

## Upgrades and migrations

The chart uses the `Recreate` deployment strategy.
The old pod stops before Kubernetes starts the new pod.
This prevents overlap between incompatible checked generations.
The replacement causes a short outage.

The pre-install and pre-upgrade migration job runs `effectusd --migrate-only`.
Set `migrations.existingSecret` to a Secret with DDL rights.
The application Secret can use a runtime role without DDL rights.
Normal startup validates the migration version with read-only queries.

Before an upgrade, remove readiness and let HTTP handlers finish.
Let the Kafka consumer finish its current record or leave its offset uncommitted.
Set `terminationGracePeriodSeconds` above the daemon shutdown timeout.

## Secret rotation

A Secret update does not change a running process.
Set `podAnnotations` for the cluster secret-reloader, or run a controlled Helm upgrade.
Keep old and new API tokens valid during the first replacement.
Remove the old token only after all clients use the new token.
Use the same replacement process for the database DSN and gRPC TLS Secret.
