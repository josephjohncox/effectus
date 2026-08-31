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

1. Load schemas (proto or JSON schema) for `acme.v1.facts.Customer`.
2. Run your app and subscribe to facts.

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

1. Register the `acme.v1.facts.Order` schema and start the source.

## 3) S3 facts from JSON exports (stream)

1. Make sure the environment contains your AWS credentials.
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

1. Use `mappings` if you want different schemas per prefix/key pattern.
2. For Parquet objects, set `format: "parquet"`.

See `examples/warehouse_sources/` for example config files.
See `examples/warehouse_sources/s3_parquet_demo` for a runnable Parquet reader.

## 4) Postgres CDC (wal2json)

1. Install `wal2json` and set `wal_level=logical`.
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

## 6) Embedded library

Use the `embedded` package to run checked rules inside a Go service:

```go
application, err := embedded.New("orders", "1.0.0").
  AddFact("order.id", "").
  AddFact("order.total", 0.0).
  AddSource("review.eff", ruleSource).
  AddVerb(embedded.Verb{
    Name:         "RequestManualReview",
    ArgTypes:     map[string]string{"orderId": "string", "reason": "string"},
    RequiredArgs: []string{"orderId", "reason"},
    ReturnType:   "string",
    Capabilities: []string{"write", "create", "idempotent"},
    Handler:      reviewService.RequestReview,
  }).
  Build(ctx)
if err != nil {
  return err
}
defer application.Close()

result, err := application.Execute(ctx, embedded.Request{
  Namespace:      "merchant-42",
  IdempotencyKey: "order-100-created",
  Facts: map[string]any{
    "order": map[string]any{"id": "order-100", "total": 2499.00},
  },
})
```

Run the complete example:

```bash
cd examples
go run ./embedded_orders
```

The default embedded stores are process-local. Use standalone mode for restart-safe execution.

## 7) Standalone business executor

Run the complete daemon, PostgreSQL, and HTTP executor path:

```bash
examples/standalone_executor/scripts/run.sh
```

The example submits one order twice. It verifies one Effectus execution and one business review.

Read [Integration Guide](INTEGRATION.md) for the request headers, idempotency contract, and deployment structure.

## 8) Immutable OCI deployment

Create a bundle, push and sign it, then run `effectusd` with the published digest.

```bash
effectusc bundle \
  --name fraud-demo \
  --version 1.0.0 \
  --schema-dir examples/fraud_e2e/schema \
  --verb-dir examples/fraud_e2e/verbs \
  --rules-dir examples/fraud_e2e/rules \
  --oci-ref ghcr.io/myorg/bundles/fraud-demo:1.0.0

EFFECTUS_API_TOKEN="$EFFECTUS_API_TOKEN" \
EFFECTUS_POSTGRES_DSN="postgres://effectus:...@db/effectus?sslmode=require" \
effectusd \
  --oci-ref ghcr.io/myorg/bundles/fraud-demo@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
  --oci-signature-verifier /usr/local/bin/effectus-verify-oci \
  --http-addr :8080
```

Health and readiness:

```bash
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
```

UI and API (token required for `/api/*`):

```bash
open http://localhost:8080/ui
curl -H "Authorization: Bearer $EFFECTUS_API_TOKEN" http://localhost:8080/api/status
```
