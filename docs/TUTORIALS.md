# Quick Tutorials

Short, copy-pasteable walkthroughs for common Effectus workflows.

## 1) Snowflake facts via SQL adapter (batch)
1. Install the Snowflake driver in your app (for example `gosnowflake`).
2. Create a source config:

```yaml
source_id: "snowflake_customers"
type: "sql"
config:
  driver: "snowflake"
  dsn: "${SNOWFLAKE_DSN}"
  mode: "batch"
  query: "SELECT id, email, updated_at FROM CUSTOMERS"
  poll_interval: "10m"
  schema_name: "acme.v1.facts.Customer"
```

3. Load schemas (proto or JSON schema) for `acme.v1.facts.Customer`.
4. Run your app and subscribe to facts.

## 2) Iceberg facts via Trino (stream)
1. Install the Trino driver in your app.
2. Create a source config:

```yaml
source_id: "iceberg_orders"
type: "iceberg"
config:
  driver: "trino"
  dsn: "${TRINO_DSN}"
  catalog: "lakehouse"
  namespace: "sales"
  table: "orders"
  mode: "stream"
  watermark_column: "updated_at"
  poll_interval: "10s"
  schema_name: "acme.v1.facts.Order"
```

3. Register the `acme.v1.facts.Order` schema and start the source.

## 3) S3 facts from JSON exports (stream)
1. Ensure your AWS credentials are available in the environment.
2. Create a source config:

```yaml
source_id: "s3_events"
type: "s3"
config:
  region: "us-east-1"
  bucket: "acme-exports"
  prefix: "events/"
  mode: "stream"
  format: "ndjson"
  poll_interval: "5s"
  schema_name: "acme.v1.facts.Event"
```

3. Use `mappings` if you want different schemas per prefix/key pattern.
4. For Parquet objects, set `format: "parquet"`.

See `examples/warehouse_sources/` for production-style config files.
See `examples/warehouse_sources/s3_parquet_demo` for a runnable Parquet reader.

## 4) Postgres CDC (wal2json)
1. Ensure `wal2json` is installed and `wal_level=logical`.
2. Create a source config:

```yaml
source_id: "orders_cdc"
type: "postgres_cdc"
config:
  connection_string: "postgres://user:pass@localhost:5432/app_db"
  slot_name: "effectus_orders"
  plugin: "wal2json"
  create_slot: true
  poll_interval: "2s"
  schema_mapping:
    public.orders: "acme.v1.facts.OrderChange"
```

## 5) AMQP streaming
1. Create a queue and exchange.
2. Create a source config:

```yaml
source_id: "amqp_events"
type: "amqp"
config:
  url: "amqp://guest:guest@localhost:5672/"
  queue: "events"
  exchange: "events"
  routing_key: "events.*"
  format: "json"
  schema_name: "acme.v1.facts.Event"
```

## 6) Library usage (compile + execute)
This is the shortest “library mode” path: register schema/verbs, compile rules, execute against facts.

```go
package main

import (
  "context"

  "github.com/effectus/effectus-go/common"
  "github.com/effectus/effectus-go/compiler"
  "github.com/effectus/effectus-go/list"
  "github.com/effectus/effectus-go/schema/types"
  "github.com/effectus/effectus-go/schema/verb"
)

type noopExec struct{}
func (n *noopExec) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
  return true, nil
}

func main() {
  ts := types.NewTypeSystem()
  ts.RegisterFactType("order.id", types.NewStringType())
  ts.RegisterFactType("order.total", types.NewFloatType())

  registry := verb.NewRegistry(ts)
  _ = registry.RegisterVerb(&verb.Spec{
    Name:       "FlagHighValue",
    ArgTypes:   map[string]string{"orderId": "string"},
    ReturnType: "bool",
    Executor:   &noopExec{},
  })

  facts := common.NewBasicFacts(map[string]interface{}{
    "order": map[string]interface{}{"id": "o-1", "total": 2500.0},
  }, ts)

  comp := compiler.NewCompiler()
  compTS := comp.GetTypeSystem()
  compTS.RegisterFactType("order.id", types.NewStringType())
  compTS.RegisterFactType("order.total", types.NewFloatType())
  compTS.RegisterVerbType("FlagHighValue", map[string]*types.Type{"orderId": types.NewStringType()}, types.NewBoolType())

  parsed, _ := comp.ParseAndTypeCheck("rules/flags.eff", facts)
  listCompiler := &list.Compiler{}
  specAny, _ := listCompiler.CompileParsedFile(parsed, "rules/flags.eff", facts.Schema())
  spec := specAny.(*list.Spec)
  spec.VerbRegistry = registry

  _ = spec.Execute(context.Background(), facts, nil)
}
```

## 7) Bundle-only run (OCI + hot reload)
Create a bundle, push to a registry, and run `effectusd` using only the OCI reference.

```bash
effectusc bundle \
  --name fraud-demo \
  --version 1.0.0 \
  --schema-dir examples/fraud_e2e/schema \
  --verb-dir examples/fraud_e2e/verbs \
  --rules-dir examples/fraud_e2e/rules \
  --oci-ref ghcr.io/myorg/bundles/fraud-demo:1.0.0

effectusd \
  --oci-ref ghcr.io/myorg/bundles/fraud-demo:1.0.0 \
  --reload-interval 60s \
  --http-addr :8080 \
  --api-token demo-token
```

Health and readiness:

```bash
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
```

UI and API (token required for `/api/*`):

```bash
open http://localhost:8080/ui
curl -H "Authorization: Bearer demo-token" http://localhost:8080/api/status
```
