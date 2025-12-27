# effectusd Helm Chart

This chart deploys the `effectusd` runtime with an OCI bundle reference.

## Install

```bash
helm install effectusd oci://ghcr.io/myorg/helm/effectusd \
  --version 1.0.0 \
  --set bundle.ociRef=ghcr.io/myorg/bundles/fraud-demo:1.0.0
```

## Configuration

Key values in `values.yaml`:

- `image.repository` / `image.tag`
- `bundle.ociRef` (required)
- `bundle.reloadInterval`
- `api.*` (auth, ACLs, rate limits)
- `facts.*` (store path, merge strategy, cache limits)
