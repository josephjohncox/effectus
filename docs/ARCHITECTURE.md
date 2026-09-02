# Effectus Architecture

Effectus has one production path: an immutable `bundle.SourceBundle` is checked,
compiled once, and run by one daemon process.

```text
SourceBundle -> effectusc check|compile|inspect
                    |
                    v
effectusd -> runtime.CompileGeneration -> runtime.Engine
                    |                 |
                    |                 +-> resolved HTTP verb descriptors
                    v
             PostgreSQL ledger and saga outbox
                    |
                    v
             HTTP, Kafka, and generated gRPC admission
```

## SourceBundle and checked generation

A producer creates canonical SourceBundle JSON with the `bundle` package.
`effectusc check` validates it, `compile` writes checked-IR bytes, and `inspect`
reports source and IR identities. None of these commands accepts loose rule
files, extension directories, plugins, or mutable deployment state.

At startup `effectusd` loads exactly one local bundle or one digest-pinned,
signature-verified OCI bundle. It calls `runtime.CompileGeneration` once and
creates one `runtime.Engine`. Replacing rules requires a new bundle and process.
There is no rule apply, rollback, reload, or hot-load API.

Production descriptors resolve HTTP verb executors before the daemon serves.
The generated gRPC service is an inbound admission API, not an outbound executor.

## Durable admission and execution

PostgreSQL stores execution admission records, the immutable generation artifact,
and the saga outbox. The engine writes durable admission and dispatch intent
before an external invocation. Recovery uses the persisted artifact and outbox;
it does not consult a mutable configuration authority.

`POST /v1/execute` requires bearer authentication and `Idempotency-Key`. It uses
`runtime.WaitAccepted` and returns HTTP 202 only after durable admission. A
matching retry returns the same execution identity. A different payload for the
same identity, or a stale `If-Match` generation digest, returns HTTP 409.

HTTP 202 does not mean that an external verb completed. External exactly-once
behavior requires the destination to enforce the supplied idempotency key or
fencing token.

## Kafka and gRPC

Kafka records have stable delivery identities. `durable_acceptance` maps to
`WaitAccepted`; `completed_processing` maps to `WaitTerminal`. The consumer
commits an offset only after the configured boundary. DLQ publication and offset
commit are separate broker operations and can duplicate a DLQ record after a
crash.

The generated gRPC API authenticates callers and uses the same engine and
immutable generation. TLS is required unless the explicit development override
is selected.

## Deployment

The Helm chart deploys one replica with a Recreate strategy, a digest-pinned
image, OCI bundle reference, PostgreSQL and API Secrets, probes, and optional
gRPC TLS. See [Runtime Configuration](RUNTIME_CONFIG.md) and
[Runtime Guarantees](GUARANTEES.md).
