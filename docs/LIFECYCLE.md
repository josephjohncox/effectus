# Runtime Lifecycle

`effectusd` has no in-process generation lifecycle. One process owns one
immutable SourceBundle and one checked generation.

## Start

1. Load exactly one `--bundle` file or one `--oci-ref` with a signature verifier.
2. Compile the SourceBundle to a checked generation and resolve its descriptors.
3. Connect to PostgreSQL and apply or validate current migrations.
4. Configure the durable ledger, saga outbox, fencing provider, and recovery
   worker.
5. Start enabled HTTP, gRPC, or Kafka listeners.

A failure in any step prevents readiness. To change a rule, descriptor, or
schema declaration, create a new SourceBundle and replace the process.

## Admission

`POST /v1/execute` requires `Authorization: Bearer TOKEN` and an
`Idempotency-Key` header. The request body contains `namespace` and `facts`.
The daemon always uses `WaitAccepted`; HTTP 202 means PostgreSQL accepted the
execution. The same key and content returns the same execution identity. A
different payload for that identity, or a stale `If-Match` generation value,
returns HTTP 409.

Accepted work can complete later or fail during external execution. A 202 is not
an external-effect success response.

## Stop and recovery

Signal cancellation stops listeners and the recovery worker. The durable ledger
and outbox remain in PostgreSQL. A replacement daemon validates the current
schema, loads its bundle, and recovery resolves each execution's persisted
generation artifact before it resumes eligible work.

There are no candidate activation, refresh, rollback, deployment-history, or
hot-load phases in the current daemon.
