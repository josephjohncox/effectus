# External Fact Sources - Streaming + Batch Tutorials

This guide explains how to ingest facts from external systems (SQL/Snowflake, Kafka, S3/Iceberg, etc.) using the Effectus **adapters** layer. It includes end-to-end patterns, config examples, and a step-by-step for creating new adapters.

---

## Short Answer (TL;DR)
Use the **adapters layer** for external fact sources. We already ship **Kafka + Postgres poller/CDC + MySQL CDC + Redis streams + file watcher + HTTP webhook + SQL + S3 + Iceberg + AMQP + gRPC**; new sources implement `adapters.FactSource` and register via `adapters.RegisterSourceType(...)`.

---

## Core Pattern (Applies to All Sources)

### 1) Pick ingestion mode
- **Streaming:** Kafka, Postgres CDC, MySQL CDC, Redis Streams, AMQP, gRPC -> `adapters/kafka`, `adapters/postgres/cdc`, `adapters/mysql`, `adapters/redis`, `adapters/amqp`, `adapters/grpc`
- **Polling / batch:** SQL/Snowflake -> `adapters/sql`; S3/Iceberg -> dedicated adapters (`adapters/s3`, `adapters/iceberg`)

### 2) Implement a `FactSource`
Create `adapters/<source>/` with:
- `Create(config) (FactSource, error)`
- `Start / Stop / Subscribe / GetSourceSchema / HealthCheck`
- Convert raw records -> `adapters.TypedFact` (includes schema name + version)

### 3) Map external identifiers to Effectus fact types
Use `FactMapping` to map source IDs (topic/table/schema ID) to your Effectus fact type:

```go
Mappings: []adapters.FactMapping{
  {SourceKey: "snowflake.table.customer", EffectusType: "acme.v1.facts.Customer", SchemaVersion: "v1"},
}
```

### 4) Register schemas
- **JSON:** `TypeSystem.LoadJSONSchemaFile(...)`
- **Proto:** `TypeSystem.RegisterProtoTypes(...)`
- **Versioned registry:** see `schema/buf_integration.go`

Versioned facts can be registered explicitly:

```go
typeSystem.RegisterFactTypeVersion(\"acme.facts.Customer\", \"v2\", customerType, true)
```

### 4b) External schema registries (optional)
If your facts include schema IDs from an external registry (Confluent, Glue, custom), resolve them to Effectus
schema name + version at the adapter boundary. Allow overrides in config/env for local runs.

Example config shape:

```yaml
schema_registry:
  mode: "confluent"         # or "glue" / "custom"
  endpoint: "http://registry:8081"
  auth_token: "${REGISTRY_TOKEN}"
  cache_ttl: "10m"
  override_mapping:
    "schema-id-123": "acme.v1.facts.Transaction@v1"
```

### 4c) Buf registry + SQL catalogs (hot reload)
Effectus reloads schemas when `*.schema.json` files change in `extensions.dirs` or OCI bundles. For Buf or SQL‑backed
schemas, run a sync job that:

1) Pulls the latest schema source (e.g., `buf export`, SQL catalog introspection).  
2) Emits `*.schema.json` into your configured extensions directory or OCI bundle.  
3) Lets `effectusd` reload on the next interval.

This keeps schema governance external while still allowing hot‑loaded updates inside the runtime.

### 5) Merge multiple sources into one facts provider
Use namespaces + aliases for clean composition:
- `pathutil.NamespacedLoader`
- `pathutil.NewAliasedFacts(...)`

For overlapping paths from multiple sources, use a merge provider with an explicit strategy:

```go
merged := pathutil.NewMergedFactProvider([]pathutil.SourceProvider{
  {Name: "stream", Provider: streamFacts, Priority: 100},
  {Name: "warehouse", Provider: warehouseFacts, Priority: 10},
}, pathutil.MergeFirst) // or MergeLast / MergeError
```

When conflicts occur, `GetWithContext` reports all sources that had values.

### 6) Universes + namespaces (scoping rules)
Use universes to isolate facts per tenant, environment, or domain while keeping the same rule bundle.

```go
registry := pathutil.NewUniverseRegistry()

// Register a merged provider for the \"customer\" namespace in two universes.
registry.RegisterMerged(\"prod\", \"customer\", []pathutil.SourceProvider{
  {Name: \"stream\", Provider: streamFacts, Priority: 100},
  {Name: \"warehouse\", Provider: warehouseFacts, Priority: 10},
}, pathutil.MergeFirst)

registry.Register(\"staging\", \"customer\", stagingFacts)

facts := pathutil.NewUniverseFacts(registry, \"prod\", typeSystem)
```

Namespace-based rules (ex: `customer.tier == \"gold\"`) now resolve within the selected universe.

---

## Using Adapters (Quick Start)

```go
import (
  "context"
  "github.com/effectus/effectus-go/adapters"
  _ "github.com/effectus/effectus-go/adapters/kafka"
)

cfg := adapters.SourceConfig{
  SourceID: "events",
  Type: "kafka",
  Config: map[string]interface{}{
    "brokers": []string{"localhost:9092"},
    "topic": "events",
    "consumer_group": "effectus",
    "schema_format": "json",
  },
  Mappings: []adapters.FactMapping{
    {SourceKey: "schema-id-123", EffectusType: "acme.v1.facts.Transaction", SchemaVersion: "v1"},
  },
}

source, _ := adapters.CreateSource(cfg)
_ = source.Start(context.Background())
ch, _ := source.Subscribe(context.Background(), []string{"acme.v1.facts.Transaction"})
for fact := range ch {
  // fact.Data is a proto.Message, convert to map if needed
}
```

---

## Tutorials by Source

### Kafka (Streaming)
Kafka is already supported by `adapters/kafka`. Typical setup:

```yaml
source_id: "kafka_events"
type: "kafka"
config:
  brokers: ["localhost:9092"]
  topic: "events"
  consumer_group: "effectus"
  schema_format: "json"
mappings:
  - source_key: "schema-id-123"  # or topic name if no schema registry
    effectus_type: "acme.v1.facts.Transaction"
    schema_version: "v1"
```

**Schema registry integration:** store the schema ID in headers (e.g., `schema-id`) and map it via `FactMapping`. For a real registry, add a converter in the adapter that looks up schemas and populates `EffectusType` dynamically.

---

### Postgres CDC (Streaming)
`adapters/postgres/cdc.go` supports logical replication with a CDC transformer.

```yaml
source_id: "postgres_cdc"
type: "postgres_cdc"
config:
  connection_string: "postgres://user:pass@host/db"
  slot_name: "effectus_slot"
  publication_name: "effectus_pub"
  tables: ["public.orders"]
  operations: ["INSERT", "UPDATE", "DELETE"]
  schema_mapping:
    public.orders: "acme.v1.facts.OrderChange"
```

Use CDC when you need near-real-time updates without polling.

Notes:
- Requires a logical decoding plugin like `wal2json`.
- Use `create_slot: true` to have Effectus create the slot automatically.

---

### MySQL CDC (Streaming)
`adapters/mysql` streams binlog row events.

```yaml
source_id: "mysql_cdc"
type: "mysql_cdc"
config:
  host: "localhost"
  port: 3306
  user: "replicator"
  password: "secret"
  server_id: 100
  flavor: "mysql"
  tables: ["app.orders"]
  schema_mapping:
    app.orders: "acme.v1.facts.OrderChange"
```

---

### AMQP (Streaming)
Consume RabbitMQ or AMQP queues.

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

---

### gRPC Streaming
Consume a server-streaming RPC that emits `google.protobuf.Struct`.

```yaml
source_id: "grpc_events"
type: "grpc"
config:
  address: "localhost:9000"
  method: "/acme.v1.Facts/StreamFacts"
  tls: false
  schema_name: "acme.v1.facts.Event"
```

Note: the adapter expects both request and response types to be `google.protobuf.Struct`.

---

### SQL / Snowflake / Trino / Athena (Batch + Streaming)
Use the **generic SQL adapter** (`adapters/sql`). It supports both **batch** and **stream** modes.

#### Batch mode (snapshots)
```yaml
source_id: "warehouse_snapshot"
type: "sql"
config:
  driver: "snowflake"         # or "postgres", "mysql", "trino", etc.
  dsn: "${SNOWFLAKE_DSN}"
  mode: "batch"
  query: "SELECT id, email, updated_at FROM customers"
  poll_interval: "10m"
  schema_name: "acme.v1.facts.Customer"
  schema_version: "v1"
```

#### Streaming mode (watermark incremental)
```yaml
source_id: "warehouse_stream"
type: "sql"
config:
  driver: "snowflake"
  dsn: "${SNOWFLAKE_DSN}"
  mode: "stream"
  stream_query: "SELECT id, email, updated_at FROM customers WHERE updated_at > ? ORDER BY updated_at ASC"
  watermark_column: "updated_at"
  start_watermark: "2025-01-01T00:00:00Z"
  watermark_type: "time"         # string | int | float | time
  poll_interval: "5s"
  schema_name: "acme.v1.facts.Customer"
  schema_version: "v1"
```

**Driver note:** you must import the matching `database/sql` driver in your app (Snowflake, MySQL, Trino, etc.).

See `examples/warehouse_sources/sql_scheduled_scrape.yaml` for a mocked scheduled scrape config (batch + incremental).

---

### S3 (Batch or Streaming)
Use the `s3` adapter for JSON/NDJSON/Parquet facts stored in buckets.

1) Poll for new objects (prefix + timestamp/ETag).
2) Read JSON/NDJSON/Parquet -> map -> TypedFact.
3) Use `schema_name` to map to an Effectus fact schema.

Recommended config shape:

```yaml
source_id: "s3_customer_dumps"
type: "s3"
config:
  bucket: "my-bucket"
  prefix: "exports/customers/"
  format: "json"             # or "ndjson" or "parquet"
  poll_interval: "15m"
  schema_name: "acme.v1.facts.Customer"
```

The adapter uses the standard AWS credential chain; set `endpoint` + `force_path_style` for MinIO/R2.
For other columnar formats, use a SQL front-end (Athena/Trino) + the SQL adapter or build a custom adapter.

---

### Iceberg (Batch or Streaming)
Use the `iceberg` adapter when Iceberg tables are queryable via SQL engines (Trino/Athena/Spark).

```yaml
source_id: "iceberg_orders"
type: "iceberg"
config:
  driver: "trino"
  dsn: "${TRINO_DSN}"
  catalog: "lake"
  namespace: "sales"
  table: "orders"
  mode: "stream"
  watermark_column: "updated_at"
  poll_interval: "10s"
  schema_name: "acme.v1.facts.Order"
```

---

## Building Your Own Adapter (Step-by-Step)

1) **Create the package** `adapters/<source>/` and implement `adapters.FactSource`.
2) **Parse config** in a `Factory` and register it in `init()`.
3) **Emit TypedFact** with schema name/version and raw payload.

Skeleton:

```go
type CustomSource struct { /* connections + config */ }

func (s *CustomSource) Start(ctx context.Context) error { /* connect */ }
func (s *CustomSource) Stop(ctx context.Context) error { /* close */ }
func (s *CustomSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
    // poll or stream, then emit TypedFact
}
func (s *CustomSource) GetSourceSchema() *adapters.Schema { /* describe fields */ }
func (s *CustomSource) HealthCheck() error { /* probe */ }
func (s *CustomSource) GetMetadata() adapters.SourceMetadata { /* basic info */ }

func init() {
  adapters.RegisterSourceType("custom", &Factory{})
}
```

---

## Merging Multiple Sources

If you ingest from multiple systems, merge using namespaces + aliases:

```go
loader := pathutil.NewNamespacedLoader()
loader.AddSource("kafka", kafkaPayload)
loader.AddSource("warehouse", sqlPayload)
provider, _ := loader.Load()

aliases := map[string]string{
  "events": "kafka",
  "customer": "warehouse.customer",
}
facts := pathutil.NewAliasedFacts(provider, aliases)
```

This keeps your rules stable even if sources change.

---

## What Exists Today (Code Paths)
- Kafka: `adapters/kafka`
- Postgres poller: `adapters/postgres/poller.go`
- Postgres CDC: `adapters/postgres/cdc.go`
- MySQL CDC: `adapters/mysql`
- Redis streams, file watcher, HTTP webhook
- SQL adapter (batch + streaming): `adapters/sql`
- S3 adapter (batch + streaming): `adapters/s3`
- Iceberg adapter (batch + streaming): `adapters/iceberg`
- AMQP adapter: `adapters/amqp`
- gRPC adapter: `adapters/grpc`

Example configs: `examples/warehouse_sources/` (devstack in `examples/warehouse_sources/devstack/`)

---

## FAQ / Best Practices

- **Use batch for large, infrequent snapshots** (S3 exports, nightly warehouse dumps).
- **Use streaming or CDC for near-real-time decisions.**
- **Keep schemas versioned** and enforce compatibility (see `schema/buf_integration.go`).
- **Accept multiple schema sources** (proto, JSON schema, registry IDs) and normalize via mappings.
- **Normalize names** with aliases so rule files don't change when sources change.

---
