# Effectus Architecture

Effectus has one production path: startup compiles an immutable `bundle.SourceBundle`
into the active generation for new admissions.
The engine can also resolve pinned historical generations for replay and recovery.

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

HTTP 202 does not mean that an external verb completed.
Destination deduplication must coordinate with the business commit to prevent duplicate effects.
Fencing rejects stale authority. It does not replace business idempotency.
Effectus does not claim exactly-once destination effects from metadata alone.

## Kafka and gRPC

Kafka records have stable delivery identities. `durable_acceptance` maps to
`WaitAccepted`; `completed_processing` maps to `WaitTerminal`. The consumer
commits an offset only after the configured boundary. DLQ publication and offset
commit are separate broker operations and can duplicate a DLQ record after a
crash.

The generated gRPC API authenticates callers and uses the same engine and
immutable generation. TLS is required unless the explicit development override
is selected.

## Shutdown and replacement

The daemon prepares listeners before starting services and cleans up partial preparation failures.
Shutdown stops admission and cancels intake workers. It then drains and joins entered handlers and workers.
The engine closes owned generations and resolver resources before the daemon closes the borrowed database.

Drain deadlines trigger cancellation, not forced Go callback termination.
Non-cooperative callbacks can prolong shutdown. The daemon keeps dependencies open until those callbacks return.
Pending durable work remains available for recovery after process replacement.
See [Runtime Lifecycle](LIFECYCLE.md) for the complete sequence.

## Deployment

The Helm chart deploys one replica with a Recreate strategy, a digest-pinned
image, OCI bundle reference, PostgreSQL and API Secrets, probes, and optional
gRPC TLS. See [Runtime Configuration](RUNTIME_CONFIG.md) and
[Runtime Guarantees](GUARANTEES.md).
