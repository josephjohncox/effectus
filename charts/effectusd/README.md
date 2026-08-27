# effectusd Helm Chart

This chart deploys the `effectusd` runtime with an OCI bundle reference.

## Install

```bash
helm install effectusd oci://ghcr.io/OWNER/helm/effectusd \
  --version 1.0.0 \
  --set image.repository=ghcr.io/OWNER/effectusd \
  --set image.tag=1.0.0 \
  --set bundle.ociRef=ghcr.io/OWNER/bundles/fraud-demo:1.0.0 \
  --set runtime.allowLegacyExecution=true \
  --set api.existingSecret=effectusd-api
```

## Configuration

Key values in `values.yaml`:

- `image.repository` / `image.tag`
- `runtime.allowLegacyExecution` (required for legacy list/flow bundles)
- `bundle.ociRef` (required)
- `bundle.reloadInterval`
- `api.*` (auth, ACLs, rate limits)
- `facts.*` (store path, merge strategy, cache limits)
- `initContainers`, `extraVolumes`, `extraVolumeMounts` (for pulling plugins or sidecar data)
- `config.*` (mount a config map and pass `--config`)

### ConfigMap usage

```yaml
image:
  tag: "1.0.0"
api:
  authMode: "token"
  existingSecret: "effectusd-api"
config:
  enabled: true
  contents: |
    bundle:
      oci: "ghcr.io/OWNER/bundles/fraud-demo:1.0.0"
    http:
      addr: ":8080"
    api:
      auth: "token"
```

Create `effectusd-api` as a Kubernetes Secret with the keys `api-token` and,
optionally, `api-read-token`. Do not put token values in the ConfigMap.
```
