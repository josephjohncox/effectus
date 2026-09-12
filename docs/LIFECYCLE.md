# Runtime Lifecycle

`effectusd` builds one active generation for new admissions at startup.
It does not replace that generation in place.
The same process can resolve pinned historical generations for replay and recovery.

## Start

1. Load exactly one `--bundle` file or one digest-pinned `--oci-ref` with signature verification.
2. Compile the source bundle and resolve its executor descriptors.
3. Connect to PostgreSQL and apply or validate migrations.
4. Configure the durable ledger, saga outbox, fencing provider, and recovery worker.
5. Prepare the enabled HTTP and gRPC listeners and the Kafka source.
6. Start the configured services.

A failure prevents successful startup. Partial preparation closes listeners already acquired and releases owned resources.
The daemon does not expose an unchecked generation while a replacement compiles.

To change a rule, descriptor, or schema declaration, create a new source bundle and replace the process.
Keep historical artifacts and required resolver support available for pending work.

## Admission

`POST /v1/execute` requires `Authorization: Bearer TOKEN` and an `Idempotency-Key` header.
The body contains a nonblank `namespace` and an object-valued `facts` field.
`universe` remains a namespace alias. Conflicting alias values fail.

The daemon's HTTP execution route uses `WaitAccepted`.
HTTP 202 acknowledges a durable execution identity, not external-effect success.
The identity includes namespace, idempotency key, ruleset, and version.
A matching retry returns the existing identity. A conflicting payload returns HTTP 409.

An optional `If-Match` digest constrains the requested generation identity.
For replay, that is the pinned historical generation, not necessarily the active generation.
A mismatch returns HTTP 409. An omitted digest does not force replay onto the active generation.

Generated gRPC and Kafka use the same engine but expose their own wait-mode contracts.
See [the gRPC matrix](grpc-capabilities.md) and [Runtime Configuration](RUNTIME_CONFIG.md).

## Stop

1. Stop HTTP admission.
2. Cancel recovery and Kafka intake.
3. Start gRPC shutdown while admitted HTTP handlers use their shutdown grace period.
4. Cancel remaining request contexts and close HTTP connections when that grace expires.
5. Join HTTP handlers, gRPC shutdown, and all intake and recovery workers.
6. Close the engine and its owned generations and resolver resources.
7. Close the database.

The HTTP shutdown timeout defaults to 30 seconds.
gRPC uses its execution-duration limit before forced cancellation, then waits for handlers to return.
These limits trigger cancellation. They are not guaranteed bounds on total shutdown time.

A Go callback that ignores cancellation can keep shutdown waiting.
The daemon does not close dependencies while entered callbacks still use them.
Do not call engine closure from an executor or server shutdown from one of its handlers.

Shutdown drains entered calls. It does not promise to complete every durable accepted execution before exit.
The ledger and outbox retain pending work for recovery.
A service failure starts the same cleanup sequence and retains operation errors instead of reporting every closure as normal cancellation.

## Replacement and recovery

A replacement daemon loads its startup bundle and validates or migrates the database schema.
Recovery consults persisted state and resolves each execution's pinned generation artifact before resuming eligible work.

An active recovery lease is authority, not metadata that another caller can impersonate.
Expired or lost authority must not complete a dispatch or execution.
An unknown destination outcome blocks by default.
A checked `SINK_GUARANTEED` step can retry a valid unknown outcome while its attempt budget remains, using the same stable identity.
`KEY_REQUIRED` alone does not authorize that retry. Unauthorized or exhausted unknown outcomes remain blocked.
The declared policy does not provide destination deduplication by itself. See [unknown-outcome handling](DURABLE_SAGA_PROTOCOL.md#unknown-outcomes).

There are no candidate-activation, refresh, deployment-history, automatic rollback, or hot-load phases in the current daemon.
See [Runtime Guarantees](GUARANTEES.md) for destination idempotency and fencing obligations.
