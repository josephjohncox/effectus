# Runtime Configuration

This page describes `effectusd`, not Go library options.
The daemon reads command-line flags and the supported environment variables below.
It does not load a YAML or JSON runtime configuration file.
A source bundle is executable input, not a runtime configuration file.

## Prerequisites

Serve mode requires:

- An immutable source bundle, available locally or through a verified digest-pinned OCI reference
- PostgreSQL with the required migrations
- Destination services and credentials appropriate for the bundle's HTTP descriptors
- An API bearer token when HTTP or gRPC is enabled

Set secrets through your deployment environment:

```bash
export EFFECTUS_POSTGRES_DSN='postgres://user:password@database/effectus?sslmode=require'
export EFFECTUS_API_TOKEN='replace-with-a-secret'
```

These are placeholders, not working credentials.
`--postgres-dsn` overrides the DSN environment variable. Prefer the environment to avoid a DSN in the process argument list.
There is no API-token command-line flag or daemon authentication-disable setting.

## Migrations

Validate a prepared database without starting services:

```bash
effectusd --mode=migrate --database-migrations=validate
```

Apply migrations as an explicit operator action:

```bash
effectusd --mode=migrate --database-migrations=apply
```

Migration mode accepts no bundle input and does not require an API token.
`--migrate-only` remains an apply-and-exit alias. Do not combine it with explicit `--mode`.
The default serve-mode migration policy is `validate`.

## Local source bundle

Create a bundle with `bundle.New` or use the [getting-started path](GETTING_STARTED.md).
The following commands assume `order-review.json` already exists and the prerequisite environment is exported:

```bash
effectusc check --bundle order-review.json
effectusd --mode=serve --bundle order-review.json --http-addr=127.0.0.1:8080
```

Serve mode requires exactly one `--bundle` or `--oci-ref` input.
The daemon compiles and resolves the startup generation before starting services.
A changed bundle requires process replacement.

## Verified OCI source bundle

OCI input requires a digest-pinned reference and a verifier executable.
The verifier receives the repository reference and verified digest as separate arguments.
Set `BUNDLE_REF` to your published digest reference. Do not use a mutable tag.

```bash
: "${EFFECTUS_POSTGRES_DSN:?set the PostgreSQL DSN}"
: "${EFFECTUS_API_TOKEN:?set the API bearer token}"
: "${BUNDLE_REF:?set a digest-pinned OCI reference}"
effectusd --mode=serve --oci-ref="$BUNDLE_REF" \
  --oci-signature-verifier=/usr/local/bin/effectus-verify-oci
```

The verifier defines operator trust policy. The daemon rejects unverified content.
It does not accept extension directories, in-process plugins, or reload configuration.

## HTTP admission

HTTP defaults to `:8080`. Use `--http-addr=` to disable it.
All `/v1/*` requests require `Authorization: Bearer TOKEN`.
`/healthz` and `/readyz` remain unauthenticated probes.

`POST /v1/execute` also requires `Idempotency-Key`, a nonblank namespace, and object-valued facts.
It uses accepted-only execution. HTTP 202 is not a business-completion result.
An explicit `If-Match` checks the active generation for new admission or the pinned historical generation for replay.

`--http-shutdown-timeout` defaults to 30 seconds. Zero also selects 30 seconds. Negative values fail.
Expiry cancels remaining handlers, but shutdown still joins them before closing dependencies.
HTTP does not inherit gRPC TLS settings. Use an appropriate TLS-terminating boundary outside local development.

## Inbound gRPC

gRPC is disabled unless `--grpc-addr` is nonblank.
It uses the same API bearer token and requires TLS by default:

```bash
: "${EFFECTUS_POSTGRES_DSN:?set the PostgreSQL DSN}"
: "${EFFECTUS_API_TOKEN:?set the API bearer token}"
effectusd --bundle order-review.json --http-addr= --grpc-addr=127.0.0.1:9091 \
  --grpc-tls-cert=/run/tls/tls.crt --grpc-tls-key=/run/tls/tls.key
```

`--grpc-allow-insecure` is a local-development plaintext override. It does not disable authentication.
Do not combine it with a TLS certificate or key.

The daemon uses 4 MiB receive/send limits, a 30-second execution limit, and 128 concurrent RPCs.
Those limits have Go library options but no daemon flags or environment overrides.
See [gRPC Execution](GRPC_EXECUTION.md) for the exact separation.

## Kafka intake

Kafka is an inbound fact source, not an outbound executor.
The daemon supplies PostgreSQL-backed delivery tracking.
Use a stable consumer group.
The daemon currently fixes `SourceID` to `effectusd` and `ClusterNamespace` to `default`, without flags to override them.
Do not assume that different broker lists create different delivery-identity namespaces in a shared database.

```bash
: "${EFFECTUS_POSTGRES_DSN:?set the PostgreSQL DSN}"
: "${EFFECTUS_API_TOKEN:?set the API bearer token}"
effectusd --bundle order-review.json --fact-source=kafka \
  --kafka-brokers=kafka-1:9092,kafka-2:9092 \
  --kafka-topic=facts --kafka-consumer-group=effectusd-production \
  --kafka-ack-contract=completed_processing
```

HTTP remains enabled unless explicitly disabled, so this example requires the API token.
The acknowledgement contract defaults to `completed_processing`.
`durable_acceptance` commits offsets after durable admission instead of terminal processing.
Poison handling defaults to halt. The daemon does not expose every library tracker or Kafka policy setting as a flag.

## Outbound executor settings

The production daemon resolves HTTP executor descriptors only.
Outbound URL, authentication headers, request timeout, and response-size policy belong to those descriptors.
The HTTP resolver defaults to POST, a 30-second timeout, a 1 MiB response limit, and private-network access disabled.
Neither inbound gRPC options nor Kafka intake settings configure outbound verbs.

For the full flag inventory, see [CLI Commands](COMMANDS.md).
