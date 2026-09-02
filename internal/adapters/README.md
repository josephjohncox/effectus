# Source Adapters

The `adapters` tree contains library adapters for external fact sources.

Effectusd uses dedicated production admission paths for HTTP and Kafka. Other adapters are embedding components and examples unless a deployment wires them explicitly.

## Packages

| Package | Source |
| --- | --- |
| `amqp` | AMQP deliveries |
| `bufschema` | Buf schema registry |
| `files` | Local file changes |
| `grpc` | Server-streaming gRPC |
| `http` | HTTP webhook requests |
| `iceberg` | Iceberg queries |
| `kafka` | Kafka consumer groups |
| `mysql` | MySQL binlog changes |
| `postgres` | PostgreSQL polling and logical decoding |
| `redis` | Redis streams |
| `s3` | S3 objects |
| `sql` | Generic SQL queries |

Read [Fact Sources](../docs/FACT_SOURCES.md) for setup examples.

## Library interface

A library source implements `adapters.FactSource`:

```go
type FactSource interface {
    Subscribe(ctx context.Context, factTypes []string) (<-chan *TypedFact, error)
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    GetSourceSchema() *Schema
    HealthCheck() error
    GetMetadata() SourceMetadata
}
```

`TypedFact` carries a schema name, a protobuf message, the original bytes, source metadata, and tracing identifiers.

Some sources set `TypedFact.Acknowledge`. Call it only after the fact reaches the configured durable processing boundary. An uncalled callback leaves the source record available for redelivery.

This interface does not provide durable admission by itself. An embedding application must connect accepted facts to the execution engine.

## Production HTTP admission

The HTTP adapter applies authentication, request limits, queue limits, and graceful shutdown.

A full queue returns HTTP 503. The source does not acknowledge a request that it did not accept.

Use explicit authentication in production. Constant-time checks protect configured shared credentials.

## Production Kafka admission

The Kafka source uses a consumer group and one stable delivery identity per cluster namespace, topic, partition, and offset.

The handler selects one acknowledgement contract:

- `durable_acceptance` commits after the engine records durable admission.
- `completed_processing` commits after the selected execution completes.

The source retries handler failures with bounded backoff. Poison policies can halt, skip after durable acknowledgement, or publish to a DLQ.

DLQ publication and source-offset commit are not transactional. A process stop between them can duplicate the DLQ record.

The checked daemon input is a JSON envelope:

```json
{
  "namespace": "tenant-a",
  "universe": "orders",
  "facts": {
    "order": {
      "id": "order-1",
      "total": 42
    }
  }
}
```

The legacy `MessageConverter` supports mapped JSON objects and emits `google.protobuf.Struct`.

It rejects protobuf input because no descriptor-backed decoder is configured. This failure occurs during source configuration.

## CDC adapters

PostgreSQL logical decoding requires an installed output plugin, such as `wal2json`.

Incremental PostgreSQL polling requires a globally unique tie-break column and a durable processed-key ledger. Create the configured ledger before the poller starts:

```sql
CREATE TABLE effectus_poller_processed (
    source_id text NOT NULL,
    record_key text NOT NULL,
    processed_at timestamptz NOT NULL,
    PRIMARY KEY (source_id, record_key)
);
```

Set `processed_ledger_table` to this table. The poller rescans source rows and excludes durable processed keys. This prevents delayed lower commits from being skipped.

MySQL CDC requires binlog access and a replication-capable account.

Both adapters depend on database retention and privilege policy. Review those settings before production use.

## Warehouse adapters

The SQL, S3, and Iceberg adapters support batch or polling integrations.

A query or object scan can return large data sets. Configure source limits and downstream admission limits together.

## Security rules

- Put credentials in a secret store.
- Use TLS for remote sources.
- Restrict database and object-store privileges.
- Validate source payloads before execution.
- Set body, message, query, and object-size limits.
- Treat adapter health as a readiness dependency only when the deployment requires it.

## Testing

Run unit tests:

```bash
go test ./internal/adapters/...
```

Run the PostgreSQL CDC tests:

```bash
docker compose -f tests/fixtures/cdc-stack/docker-compose.yml up -d
POSTGRES_DSN='postgres://effectus:effectus@localhost:5432/effectus_cdc?sslmode=disable' \
  go test -race -tags=integration ./internal/adapters/postgres
```

Run the Redis Streams tests:

```bash
docker compose -f tests/fixtures/durable-stack/docker-compose.yml up -d
REDIS_ADDR=localhost:56379 go test -race -tags=integration ./internal/adapters/redis
```

S3 integration tests require an explicitly configured compatible endpoint. They do not ship a local product example or test fixture.

Stop each test fixture with `docker compose down -v` after use.
