# Runtime Configuration (Non‑Library Mode)

Use a YAML (or JSON) config to run the `effectusd` compatibility runtime without embedding Effectus in a Go program.
This path executes legacy callback-based list and flow specifications and requires `--allow-legacy-execution`. It is not a checked IR production path.

Run with:

```bash
EFFECTUS_API_TOKEN="..." EFFECTUS_API_READ_TOKEN="..." \
  effectusd --config effectusd.yaml --allow-legacy-execution
```

## Example: Mixed HTTP + OCI verb sources

```yaml
bundle:
  oci: "ghcr.io/myorg/bundles/fraud-demo:1.0.0"
  reload_interval: "60s"

http:
  addr: ":8080"
metrics:
  addr: ":9090"

api:
  auth: "token"
  rate_limit: 120
  rate_burst: 60
  hotload_rules: false

facts:
  store: "file"
  path: "./data/facts.json"
  merge_default: "last"
  merge_namespace:
    customer: "first"
  cache:
    policy: "lru"
    max_universes: 200
    max_namespaces: 50

schema_sources:
  - name: "fraud-db"
    type: "sql_introspect"
    namespace: "fraud"
    version: "v1"
    config:
      driver: "postgres"
      dsn: "postgres://user:pass@localhost:5432/fraud?sslmode=disable"
      schema: "public"
      table: "transactions"
      schema_name: "transaction"

  - name: "buf-registry"
    type: "buf"
    namespace: "acme"
    version: "v2"
    config:
      module: "buf.build/acme/facts"
      schema_dir: "schemas"

extensions:
  # Local extension manifests (HTTP/stream/gRPC targets)
  dirs:
    - "./extensions"

  # OCI bundles that contain *.verbs.json / *.schema.json
  oci:
    - "ghcr.io/myorg/extension-bundles/payments@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

  # Optional: hot-reload extension schemas + verbs
  reload_interval: "60s"

verbs:
  # Optional: Go plugin executors (.so)
  duplicate_policy: "error" # error | replace | ignore
  oci_warmup: false
  strict: true
  plugin_dirs:
    - "./plugins"

fixed_time: "" # Optional RFC3339 timestamp for deterministic runs
```

## Kafka fact ingestion

Use a stable consumer group and cluster namespace.
The daemon supports the completed-processing contract.
It rejects durable acceptance until checked execution has durable fact admission.

```yaml
fact_source: "kafka"
kafka:
  brokers:
    - "kafka-1:9092"
    - "kafka-2:9092"
  topic: "facts"
  consumer_group: "effectusd-production"
  cluster_namespace: "production-kafka"
  ack_contract: "completed_processing"
  max_attempts: 5
  retry_initial: "1s"
  retry_max: "30s"
  poison_policy: "halt"
  delivery_ledger: "/data/kafka-deliveries.jsonl"
```

The durable delivery ledger increments the stable delivery attempt before each handler call. Attempt limits therefore survive rebalances and process restarts.
The default poison policy leaves the failed offset uncommitted and stops the daemon.
For `skip`, the same ledger records and deduplicates the poison acknowledgement.
For `dlq`, set `dlq_topic` to a Kafka topic.
Effectus waits for DLQ publication before it commits the source offset.

Each message value uses this JSON shape:

```json
{
  "universe": "default",
  "namespace": "tenant-a",
  "facts": {
    "order": {
      "id": "order-42"
    }
  }
}
```

## Compatibility deployment example

Use this as a starting point only for an explicitly accepted legacy deployment (bundle from OCI, persisted facts, ACLs, hotload, metrics):

```yaml
bundle:
  oci: "ghcr.io/myorg/bundles/fraud-demo:1.0.0"
  reload_interval: "60s"

http:
  addr: ":8080"
metrics:
  addr: ":9090"

# Supply EFFECTUS_API_TOKEN and EFFECTUS_API_READ_TOKEN through the secret manager.
api:
  auth: "token"
  acl_file: "/etc/effectus/acl.yaml"
  rate_limit: 300
  rate_burst: 120
  hotload_rules: true

facts:
  store: "file"
  path: "/var/lib/effectus/facts.json"
  merge_default: "last"
  cache:
    policy: "lru"
    max_universes: 500
    max_namespaces: 100

schema_sources:
  - name: "warehouse"
    type: "sql_introspect"
    namespace: "warehouse"
    version: "v1"
    config:
      driver: "postgres"
      dsn: "postgres://effectus:effectus@db:5432/warehouse?sslmode=disable"
      schema: "public"
      table: "orders"
      schema_name: "order"

extensions:
  dirs:
    - "/etc/effectus/extensions"
  oci:
    - "ghcr.io/myorg/extension-bundles/payments@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  reload_interval: "60s"

verbs:
  duplicate_policy: "error"
  oci_warmup: true
  strict: true

# effectusd rejects legacy saga mode because it is not connected to V2.
saga:
  enabled: false
```

### Local extension manifest (HTTP verbs)

Put this file in `./extensions/external.verbs.json`:

```json
{
  "name": "ExternalAPI",
  "version": "1.0.0",
  "verbs": [
    {
      "name": "ValidateAccount",
      "description": "Calls external validation service",
      "capabilities": ["write", "idempotent"],
      "resources": [
        { "resource": "account_validation", "capabilities": ["write", "idempotent"] }
      ],
      "argTypes": { "accountId": "string" },
      "requiredArgs": ["accountId"],
      "returnType": "ValidationResult",
      "target": {
        "type": "http",
        "config": {
          "url": "https://api.validation.com/check",
          "method": "POST",
          "timeout": "5s"
        }
      }
    }
  ]
}
```

### OCI extension bundles

OCI extension bundles are directories containing `*.verbs.json` / `*.schema.json` files, pushed with an OCI tool
such as `oras`:

```bash
oras push ghcr.io/myorg/extension-bundles/payments:1.2.0 ./extensions
```

Then list the OCI reference under `extensions.oci`.

## Notes

- CLI flags override config values when both are provided.
- `/api/*` endpoints require a token; `/healthz` and `/readyz` are open by default.
- Set `api.hotload_rules` to enable `/api/rules/validate` and `/api/rules/hotload` (UI rule editor + VS Code hot reload).
- If you need in‑process Go executors, use `verbs.plugin_dirs` or embed via library mode.
- Extension reloading re-reads `*.verbs.json` / `*.schema.json` from disk or OCI; Go plugins are not hot-reloadable.
- Schema sources are loaded in-memory at startup; set `extensions.reload_interval` (or `bundle.reload_interval`) to poll for updates.
- `verbs.duplicate_policy` controls how duplicate verb names are resolved; `verbs.oci_warmup` prefetches OCI verb bundles at startup.
- `verbs.strict` controls runtime argument and return checks. The default is `true`. Use `false` only for unchecked development code.
- `fixed_time` pins deterministic time for expression evaluation (useful for tests and canary runs).
- Use `saga.redis` or `saga.postgres` to persist saga state when `saga.enabled` is true.

## External Schema Sources (Buf, SQL, Catalogs)

Use `schema_sources` to load schemas directly at startup (and optionally on reload). The built-in providers are:

- `sql_introspect`: Reads `information_schema` (Postgres/MySQL) or `PRAGMA table_info` (SQLite drivers) and emits a
  JSON schema from table columns.
- `buf`: Runs `buf export` and reads generated `*.schema.json` / `*.jsonschema` files (or `schema_dir`/`schema_files`
  you provide). If no JSON schemas are present, it falls back to `buf build` to derive JSON schemas from proto
  descriptors (requires `buf` on PATH).

If your registry only exposes protobuf, the `buf` provider can now generate JSON schemas from descriptors; you can
still supply a custom generator and point `schema_dir` at the output if you need tighter control.

## Kubernetes (ConfigMap)

Create a ConfigMap with the runtime YAML and mount it into the pod:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: effectusd-config
data:
  effectusd.yaml: |
    bundle:
      oci: "ghcr.io/myorg/bundles/fraud-demo:1.0.0"
      reload_interval: "60s"
    http:
      addr: ":8080"
    api:
      auth: "token"
      token: "write-token"
```

Deployment snippet:

```yaml
containers:
  - name: effectusd
    image: ghcr.io/myorg/effectusd:1.0.0
    args:
      - "--config=/etc/effectus/effectusd.yaml"
    volumeMounts:
      - name: config
        mountPath: /etc/effectus
volumes:
  - name: config
    configMap:
      name: effectusd-config
```

## Prometheus scrape

Expose `metrics.addr` and scrape `/metrics`:

```yaml
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "9090"
  prometheus.io/path: "/metrics"
```
