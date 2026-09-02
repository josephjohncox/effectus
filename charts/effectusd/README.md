# effectusd Helm Chart

This chart deploys one checked `effectusd` daemon from an immutable OCI source
bundle.

## Install

```bash
helm install effectusd oci://ghcr.io/OWNER/helm/effectusd \
  --version 1.0.0 \
  --set image.repository=ghcr.io/OWNER/effectusd \
  --set image.digest=sha256:IMAGE_DIGEST \
  --set bundle.ociRef=ghcr.io/OWNER/bundles/demo@sha256:BUNDLE_DIGEST \
  --set bundle.signatureVerifier=/usr/local/bin/effectus-verify-oci \
  --set postgres.existingSecret=effectusd-postgres-dml \
  --set api.existingSecret=effectusd-api
```

The image or a mounted read-only volume must provide
`bundle.signatureVerifier`. Startup fails if signature verification fails.
`api.existingSecret` must contain `api-token` by default; the chart exposes it
as `EFFECTUS_API_TOKEN` for authenticated HTTP and gRPC requests.

## Runtime contract

The chart sets one replica and the Recreate Deployment strategy. Do not use
multiple replicas or a rolling strategy. Production rendering requires
`image.digest`; `image.unsafeAllowTag=true` is only for local development.

The Deployment passes only current daemon flags: the OCI reference and
verifier, HTTP address, migration validation, and optional gRPC TLS address,
certificate, and key. It does not support ConfigMap runtime configuration,
mutable facts stores, OCI caches, metrics, or a UI.

`/healthz` and `/readyz` are probe endpoints. All `/v1/*` endpoints require an
`Authorization: Bearer` value that matches the API token Secret.

## Migrations

The Deployment validates migration state with the DML Secret. Enable the Helm
migration Job only when the chart manages migrations:

```yaml
postgres:
  existingSecret: effectusd-postgres-dml
migrations:
  enabled: true
  existingSecret: effectusd-postgres-ddl
```

The migration Secret must differ from the runtime Secret. The Job runs as a
pre-install and pre-upgrade hook.

## gRPC

Enable gRPC only with a TLS Secret:

```yaml
grpc:
  enabled: true
  existingTLSSecret: effectusd-grpc-tls
```

The Secret defaults to `tls.crt` and `tls.key`. gRPC uses the same API token as
HTTP.

## Render tests

```bash
for file in charts/effectusd/test-values/*.yaml; do
  helm lint charts/effectusd -f "$file"
  helm template effectusd charts/effectusd -f "$file" | kubeconform -strict -summary
done
```
