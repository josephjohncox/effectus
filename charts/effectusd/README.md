# effectusd Helm Chart

This chart deploys one checked `effectusd` daemon with an immutable OCI bundle.

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

The image or a read-only volume must provide `bundle.signatureVerifier`. Startup fails when verification fails.

The release image does not include a verifier. Use an approved verifier image and pin its digest. This example installs the verifier into a read-only runtime mount.

```yaml
bundle:
  signatureVerifier: /opt/effectus-verifier/effectus-verify-oci
initContainers:
  - name: install-oci-verifier
    image: ghcr.io/OWNER/effectus-verify-oci@sha256:VERIFIER_IMAGE_DIGEST
    command: ["/bin/cp", "/usr/local/bin/effectus-verify-oci", "/verifier/effectus-verify-oci"]
    volumeMounts:
      - name: oci-verifier
        mountPath: /verifier
extraVolumes:
  - name: oci-verifier
    emptyDir: {}
  - name: oci-verifier-policy
    secret:
      secretName: effectusd-oci-verifier-policy
extraVolumeMounts:
  - name: oci-verifier
    mountPath: /opt/effectus-verifier
    readOnly: true
  - name: oci-verifier-policy
    mountPath: /etc/effectus/verifier
    readOnly: true
```

The verifier receives the OCI reference and digest as two arguments. The approved verifier must read its identity, issuer, and key policy from `/etc/effectus/verifier`.

To upgrade the verifier, review its identity and trust-policy changes. Pin the new image digest and update the policy Secret separately. Change `rolloutNonce`, then verify a known signed bundle before production use.

## Singleton rollout contract

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
The chart sets one replica and the Recreate Deployment strategy. Do not use multiple replicas or a rolling strategy.

An upgrade stops the old pod before it starts the new pod. Plan for HTTP and gRPC downtime.

Shutdown can interrupt the active Kafka record. The consumer does not commit an interrupted record. The new pod replays it.

Recreate also prevents two pods from writing the same ReadWriteOnce PVC. A volume can take time to detach and attach.

The default shutdown timeout is 30 seconds. The termination grace period is 45 seconds.

## Database migrations

The Deployment validates migration state with the DML Secret. It does not apply DDL.

Enable the Helm migration Job when this chart manages migrations:

```yaml
postgres:
  existingSecret: effectusd-postgres-dml
migrations:
  enabled: true
  existingSecret: effectusd-postgres-ddl
```

The DDL Secret must use a different PostgreSQL role. The DML role needs runtime read and write grants plus migration-version reads.

The migration Job runs as a Helm pre-install and pre-upgrade hook. Back up the database before a contract migration.

## ConfigMap mode

```yaml
image:
  digest: "sha256:IMAGE_DIGEST"
postgres:
  existingSecret: effectusd-postgres-dml
bundle:
  cacheDir: "/data/bundles"
  signatureVerifier: "/usr/local/bin/effectus-verify-oci"
api:
  existingSecret: effectusd-api
config:
  enabled: true
  contents: |
    bundle:
      oci: "ghcr.io/OWNER/bundles/fraud-demo@sha256:BUNDLE_DIGEST"
      cache_dir: "/data/bundles"
    http:
      addr: ":8080"
    api:
      auth: "token"
```

Create `effectusd-api` as a Kubernetes Secret with the keys `api-token` and, optionally, `api-read-token`. Create `effectusd-postgres` with the `dsn` key. The Deployment exposes both with `secretKeyRef`; do not put either secret in the ConfigMap.

The chart always passes its HTTP address, metrics address, database limits, shutdown timeout, and OCI cache directory.

`bundle.cacheDir` is the chart authority. It overrides `bundle.cache_dir` in the ConfigMap. The path must be writable and mounted.

The default cache path is `/data/bundles`. The chart mounts `/data` from a PVC or `emptyDir`.

Do not put API tokens or PostgreSQL DSNs in a ConfigMap. Store them in Kubernetes Secrets.

## Resource and database budgets

The default container requests 100 millicores and 128 MiB. It limits the container to one CPU and 512 MiB.

The default PostgreSQL pool allows 20 open and 5 idle connections. One migration Job can use two more connections.

Reserve operator and monitoring connections before you set the PostgreSQL role limit. Monitor pool waits and saturation.

Keep `grpc.maxConcurrent` within the CPU, memory, and database budgets.

## Secret and certificate rotation

The process loads environment credentials and the gRPC key pair once. A Secret file update does not reload them.

Change `rolloutNonce` after a Secret update:

```bash
helm upgrade effectusd ./charts/effectusd --reuse-values \
  --set-string rolloutNonce="$(date +%s)"
```

You can add an external reloader annotation through `podAnnotations`.

Use overlapping API tokens during client rotation. Keep the old PostgreSQL credential valid through the rollback window.

Use overlapping certificate validity during gRPC rotation. Verify readiness and the served certificate before you remove the old certificate.

Monitor certificate expiry. Read `docs/PRODUCTION_RUNBOOK.md` for the full rotation procedure.

## External HTTP TLS

The Service is ClusterIP by default. The daemon HTTP port does not terminate TLS.

Expose bearer-token HTTP only through a trusted TLS ingress or service mesh. Restrict direct Service access with network policy.

Terminate and rotate HTTP certificates at that trusted boundary. Accept forwarded client headers only from the approved proxy.

## Render tests

The committed fixtures cover tag, digest, ConfigMap, gRPC TLS, persistence, migration Job, and rollout settings.

```bash
for file in charts/effectusd/test-values/*.yaml; do
  helm lint charts/effectusd -f "$file"
  helm template effectusd charts/effectusd -f "$file" | kubeconform -strict -summary
done
```
