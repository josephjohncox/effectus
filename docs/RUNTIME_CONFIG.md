# Runtime Configuration (Non-Library Mode)

`effectusd` accepts one immutable `effectus.source-bundle.v1` source bundle.
The daemon compiles that bundle once at startup and rejects legacy bundle
formats, extension directories, extension OCI bundles, plugins, and reload
configuration. The source bundle contains the complete checked declarations and
executor descriptor manifest.

PostgreSQL is required for daemon admission, recovery, outbox processing, and
fencing. Set `EFFECTUS_POSTGRES_DSN` or configure `database.dsn`.

## Load a source bundle file

Build the bundle with the current `effectusc bundle` command. Deploy the output
file without changing it:

```yaml
bundle:
  file: "/etc/effectus/order-review.json"

http:
  addr: ":8080"
metrics:
  addr: ":9090"
api:
  auth: "token"
database:
  dsn: "postgres://effectus:...@db/effectus?sslmode=require"
```

Start the daemon:

```bash
EFFECTUS_API_TOKEN="..." EFFECTUS_API_READ_TOKEN="..." \
  effectusd --config effectusd.yaml
```

The file must identify `format_version: effectus.source-bundle.v1`. A legacy
OCI or directory-style bundle is rejected before the daemon opens listeners.

## Load a verified OCI source bundle

The daemon also accepts a SourceBundle OCI image only through a digest-pinned
reference. `effectusd` verifies that the fetched image digest equals the
reference, requires exactly one SourceBundle layer, and runs the fixed verifier
executable before it decodes that layer. A tag such as `:latest` is rejected.

```yaml
bundle:
  oci: "ghcr.io/myorg/bundles/order-review@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  cache_dir: "/var/lib/effectus/bundles"

http:
  addr: ":8080"
api:
  auth: "token"
database:
  dsn: "postgres://effectus:...@db/effectus?sslmode=require"
```

```bash
EFFECTUS_API_TOKEN="..." EFFECTUS_API_READ_TOKEN="..." \
  effectusd --config effectusd.yaml \
  --oci-signature-verifier /usr/local/bin/effectus-verify-oci
```

The verifier is mandatory for OCI loading. It receives the repository name and
the verified digest. Configure its trust policy outside the bundle. `bundle.cache_dir`
stores the verified canonical source bundle and must be writable. File loading
does not perform an OCI signature verification; protect the mounted file with
your deployment and filesystem controls.

## Kafka fact ingestion

Use a stable consumer group and cluster namespace. PostgreSQL is the sole
attempt and poison ledger.

```yaml
fact_source: "kafka"
kafka:
  brokers: ["kafka-1:9092", "kafka-2:9092"]
  topic: "facts"
  consumer_group: "effectusd-production"
  cluster_namespace: "production-kafka"
  ack_contract: "durable_acceptance"
  max_attempts: 5
  retry_initial: "1s"
  retry_max: "30s"
  poison_policy: "halt"
```

Each message contains `namespace`, `universe`, and `facts`. New clients should
send both identities when they differ.

## Immutable deployment rules

- Use exactly one of `bundle.file` and `bundle.oci` (or `--bundle` and
  `--oci-ref`).
- Do not configure `extensions.dirs`, `extensions.oci`, `--extensions-dir`, or
  `--extensions-oci` with a SourceBundle. The daemon rejects them.
- Do not configure `bundle.reload_interval`, `extensions.reload_interval`, or
  `--reload-interval`. Deploy a new process with a new file or OCI digest.
- Do not configure Go plugins, legacy saga stores, or Redis daemon state.
- CLI flags override configuration values when both are present.
- `/api/*` endpoints require a token; `/healthz` and `/readyz` are open by
  default.

Use [COMMANDS.md](COMMANDS.md) for the executable flag inventory and
[GUARANTEES.md](GUARANTEES.md) for the runtime boundary.
