# Runtime Configuration (Non‑Library Mode)

Use a YAML (or JSON) config to run `effectusd` without embedding Effectus in a Go program. This is the recommended
path for production deployments and for mixing multiple verb sources (HTTP, OCI, stream, plugins).

Run with:

```bash
effectusd --config effectusd.yaml
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
  token: "write-token"
  read_token: "read-token"
  rate_limit: 120
  rate_burst: 60

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
    - "ghcr.io/myorg/extension-bundles/payments:1.2.0"

  # Optional: hot-reload extension schemas + verbs
  reload_interval: "60s"

verbs:
  # Optional: Go plugin executors (.so)
  plugin_dirs:
    - "./plugins"
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
- If you need in‑process Go executors, use `verbs.plugin_dirs` or embed via library mode.
- Extension reloading re-reads `*.verbs.json` / `*.schema.json` from disk or OCI; Go plugins are not hot-reloadable.
- Schema sources are loaded in-memory at startup; set `extensions.reload_interval` (or `bundle.reload_interval`) to poll for updates.

## External Schema Sources (Buf, SQL, Catalogs)

Use `schema_sources` to load schemas directly at startup (and optionally on reload). The built-in providers are:

- `sql_introspect`: Reads `information_schema` (Postgres/MySQL) or `PRAGMA table_info` (SQLite drivers) and emits a
  JSON schema from table columns.
- `buf`: Runs `buf export` and reads generated `*.schema.json` / `*.jsonschema` files (or `schema_dir`/`schema_files`
  you provide).

If your registry only exposes protobuf, add a generator that outputs JSON schemas and point `schema_dir` at the results.
This keeps the runtime simple while still allowing schemas to be sourced dynamically from SQL catalogs or Buf registries.

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
