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

The default mode is `serve`. It requires exactly one `--bundle PATH` or `--oci-ref REF` input.
OCI input also requires `--oci-signature-verifier PATH` and a digest-pinned reference.
`--mode=migrate` accepts no bundle and honors `--database-migrations=validate|apply`.
The legacy `--migrate-only` alias applies migrations. Do not combine it with explicit `--mode`.

The daemon reads flags and supported environment variables, not runtime YAML.
An empty string in the default column means an unset value.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--bundle` | `""` | Local source-bundle JSON path. |
| `--oci-ref` | `""` | Digest-pinned OCI source-bundle reference. |
| `--oci-signature-verifier` | `""` | Executable that verifies OCI reference and digest. |
| `--postgres-dsn` | `""` | PostgreSQL DSN. Unset falls back to `EFFECTUS_POSTGRES_DSN`. |
| `--mode` | `serve` | `serve` or `migrate`. |
| `--database-migrations` | `validate` | `validate` or `apply`. |
| `--migrate-only` | `false` | Apply migrations and exit. Legacy alias. |
| `--http-addr` | `:8080` | HTTP listen address. Empty disables HTTP. |
| `--http-shutdown-timeout` | `30s` | HTTP drain grace. Zero selects 30 seconds. Negative values fail. |
| `--grpc-addr` | `""` | Inbound generated gRPC address. Empty disables gRPC. |
| `--grpc-tls-cert` | `""` | TLS certificate PEM for gRPC. |
| `--grpc-tls-key` | `""` | TLS private-key PEM for gRPC. |
| `--grpc-allow-insecure` | `false` | Local-development plaintext gRPC override. Authentication remains required. |
| `--fact-source` | `http` | `http` or `kafka`. |
| `--kafka-brokers` | `localhost:9092` | Comma-separated Kafka brokers. |
| `--kafka-topic` | `facts` | Kafka facts topic. |
| `--kafka-consumer-group` | `effectusd` | Kafka consumer group. |
| `--kafka-ack-contract` | `completed_processing` | `durable_acceptance` or `completed_processing`. |

`EFFECTUS_API_TOKEN` is required whenever HTTP or gRPC is enabled. All `/v1/*`
requests need `Authorization: Bearer TOKEN`.

`POST /v1/execute` requires `Idempotency-Key`, a nonblank namespace, and object-valued facts.
It uses accepted-only execution. Successful admission returns HTTP 202, not business completion.
A matching retry preserves identity within the same namespace, ruleset, and version.
Conflicting content or an explicit generation mismatch returns HTTP 409.
Replay checks the pinned historical generation rather than implicitly requiring the active generation.
See the [HTTP API reference](HTTP_API.md) for routes, exact JSON schemas, errors, generation constraints, and readiness limitations.

The gRPC limits are fixed daemon defaults, not additional flags.
See [gRPC Execution](./GRPC_EXECUTION.md) for separate library options and TLS requirements.

`--help` and `-h` exit successfully. Invalid flag syntax exits with code 2.
Rejected configuration or execution errors exit with code 1.
Do not pass positional arguments to the daemon.

```bash
EFFECTUS_POSTGRES_DSN="$DB_DSN" EFFECTUS_API_TOKEN="$TOKEN" \
  effectusd --bundle orders.bundle.json --http-addr :8080
```
