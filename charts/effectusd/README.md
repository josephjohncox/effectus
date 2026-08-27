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
- `bundle.ociRef` as a digest-pinned reference
- `bundle.signatureVerifier` for the fixed verifier executable
- `bundle.reloadInterval` for non-OCI development sources only
- `api.*` (auth, ACLs, rate limits)
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
