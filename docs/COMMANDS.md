# Effectus CLI Commands

Only the commands and flags listed here are supported.

## `effectusc`

`effectusc` accepts immutable `effectus.source-bundle.v1` JSON. It does not
accept loose source files or extension directories.

| Command | Required flags | Result |
| --- | --- | --- |
| `check` | `--bundle` PATH | Check the bundle and print checked-IR identity. |
| `compile` | `--bundle` PATH, `--output` PATH | Write deterministic checked-IR protobuf bytes. |
| `inspect` | `--bundle` PATH | Print source and checked-IR identities as JSON. |

Examples:

```bash
effectusc check --bundle orders.bundle.json
effectusc compile --bundle orders.bundle.json --output orders.checked.pb
effectusc inspect --bundle orders.bundle.json
```

## `effectusd`

`effectusd` starts one immutable daemon. Set exactly one bundle input:
`--bundle PATH` or `--oci-ref REF`. OCI input also requires
`--oci-signature-verifier PATH` and a digest-pinned reference.

| Flag | Meaning |
| --- | --- |
| `--bundle` | Local SourceBundle JSON path. |
| `--oci-ref` | Digest-pinned OCI SourceBundle reference. |
| `--oci-signature-verifier` | Executable that verifies OCI reference and digest. |
| `--postgres-dsn` | PostgreSQL DSN; `EFFECTUS_POSTGRES_DSN` is the alternative. |
| `--database-migrations` | `validate` (default) or `apply`. |
| `--migrate-only` | Apply current PostgreSQL migrations and exit. |
| `--http-addr` | HTTP listen address; empty disables HTTP. |
| `--grpc-addr` | Generated gRPC listen address; empty disables gRPC. |
| `--grpc-tls-cert` | TLS certificate PEM for gRPC. |
| `--grpc-tls-key` | TLS private-key PEM for gRPC. |
| `--grpc-allow-insecure` | Development-only plaintext gRPC override. |
| `--fact-source` | `http` or `kafka`. |
| `--kafka-brokers` | Comma-separated Kafka brokers. |
| `--kafka-topic` | Kafka facts topic. |
| `--kafka-consumer-group` | Kafka consumer group. |
| `--kafka-ack-contract` | `durable_acceptance` or `completed_processing`. |

`EFFECTUS_API_TOKEN` is required whenever HTTP or gRPC is enabled. All `/v1/*`
requests need `Authorization: Bearer TOKEN`.

`POST /v1/execute` requires an `Idempotency-Key` header and a JSON body with
`namespace` and `facts`. It always uses durable acceptance and returns HTTP 202.
A matching retry has the same execution identity. Changed content for the same
key, or an `If-Match` generation digest that is stale, returns HTTP 409.

```bash
EFFECTUS_POSTGRES_DSN="$DB_DSN" EFFECTUS_API_TOKEN="$TOKEN" \
  effectusd --bundle orders.bundle.json --http-addr :8080
```
