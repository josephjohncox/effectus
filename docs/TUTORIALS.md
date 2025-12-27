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
