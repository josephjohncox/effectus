# Integration Guide

Effectus has two supported execution paths.

## Embedded

Use `embedded.Open` with an immutable `bundle.SourceBundle` and an
`invocation.Registry`. `Open` compiles checked IR and creates one immutable
`runtime.Generation` and `runtime.Engine`. Resolver descriptors, not Go
callbacks, identify production executors.

Run the working order-review path:

```bash
go run ./examples/embedded_orders
```

The example executes the shared order-review scenario and repeats its
idempotency key. Its output shows one business review and equal execution IDs.

## Durable daemon

Use `effectusd` when admissions, dispatches, and recovery must survive process
restart. Build or obtain one `effectus.source-bundle.v1` document, then start:

```bash
export DB_DSN='postgres://effectus:effectus@localhost:5432/effectus?sslmode=disable'
export EFFECTUS_API_TOKEN='replace-with-a-secret'
EFFECTUS_POSTGRES_DSN="$DB_DSN" \
  EFFECTUS_API_TOKEN="$EFFECTUS_API_TOKEN" \
  go run ./cmd/effectusd --bundle order-review.bundle.json
```

The daemon resolves all descriptors before it serves HTTP, Kafka, or gRPC. It
uses one checked generation and one engine for all transports. PostgreSQL is
the durable authority. Deploy a new process to change a bundle; hotload,
history, and rollback are not supported.

The Docker onboarding validates an HTTP executor, daemon restart, idempotent
replay, and conflicting replay rejection:

```bash
examples/standalone_executor/scripts/run.sh
examples/standalone_executor/scripts/down.sh
```

## v0.3 compatibility

`compat/v03` preserves frozen request and callback vocabulary for external
consumers. It owns its callback adaptation and does not depend on the removed
root callback facade. New applications should use `bundle`, `embedded`, and
`invocation` directly.
