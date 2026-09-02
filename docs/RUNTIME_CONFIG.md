# Runtime Configuration (Non-Library Mode)

`effectusd` accepts one immutable `effectus.source-bundle.v1` source bundle. It
compiles the bundle once at startup. PostgreSQL is required for admission,
recovery, the outbox, fencing, and the Kafka delivery ledger.

## Start from a source-bundle file

Create a source bundle with a program that uses `bundle.New`, such as
`examples/standalone_executor/bundle.go`. Validate it before deployment:

```bash
go run ./examples/standalone_executor/bundle.go order-review.json executor-token
./effectusc check --bundle order-review.json
EFFECTUS_POSTGRES_DSN='postgres://effectus:...@db/effectus?sslmode=require' \
EFFECTUS_API_TOKEN='replace-with-a-secret' \
  ./effectusd --bundle order-review.json --http-addr :8080
```

`EFFECTUS_API_TOKEN` is required whenever HTTP or gRPC is enabled. Send it as
`Authorization: Bearer TOKEN` to every `/v1/*` endpoint. `/healthz` and
`/readyz` are intentionally unauthenticated probe endpoints. `GET /v1/status`
returns the active generation after authentication. `POST /v1/execute` also
requires `Idempotency-Key`; it returns HTTP 202 after durable admission. Use
`If-Match` with a generation digest when the caller must reject a stale view.

## Load a verified OCI source bundle

OCI loading requires a digest-pinned reference and a verifier executable. The
verifier receives the repository reference and verified digest.

```bash
EFFECTUS_POSTGRES_DSN='postgres://effectus:...@db/effectus?sslmode=require' \
EFFECTUS_API_TOKEN='replace-with-a-secret' \
  ./effectusd \
  --oci-ref ghcr.io/myorg/bundles/order-review@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
  --oci-signature-verifier /usr/local/bin/effectus-verify-oci
```

The daemon rejects tags, unverified OCI content, configuration files,
extension directories, plugins, and reload configuration.

## gRPC

gRPC uses the same `EFFECTUS_API_TOKEN` as a bearer token and requires TLS by
default:

```bash
./effectusd --bundle order-review.json --grpc-addr :9091 \
  --grpc-tls-cert /run/tls/tls.crt --grpc-tls-key /run/tls/tls.key
```

`--grpc-allow-insecure` is an explicit development override. It does not disable
bearer-token authentication.

## Kafka fact ingestion

Use a stable consumer group and cluster namespace. PostgreSQL is the durable
attempt ledger. A Kafka consumer will not start unless its tracker is installed.

```bash
./effectusd --bundle order-review.json --fact-source kafka \
  --kafka-brokers kafka-1:9092,kafka-2:9092 \
  --kafka-topic facts --kafka-consumer-group effectusd-production
```

The Kafka acknowledgement contract controls whether offsets commit after
durable acceptance or completed processing. Poison handling defaults to halt.
