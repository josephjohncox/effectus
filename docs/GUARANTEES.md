# Runtime Guarantees and Limits

This document states current guarantees only.

## Implemented guarantees

- `effectusc check`, `compile`, and `inspect` consume immutable SourceBundle
  documents. Checking rejects invalid rule and declaration inputs.
- `effectusd` compiles one immutable generation at startup. An admitted
  execution pins its generation artifact for recovery.
- PostgreSQL durable admission records the admission identity and canonical
  request hash. The same identity and content replays the same execution;
  changed content conflicts.
- The HTTP admission endpoint requires bearer authentication and
  `Idempotency-Key`, uses `runtime.WaitAccepted`, and returns HTTP 202 only
  after durable admission. Identity and stale-generation conflicts return 409.
- The saga outbox records dispatch intent before external invocation. Leases and
  fencing tokens prevent an expired worker from completing a newer claim.
- Kafka commits after its explicit contract: `durable_acceptance` waits for
  durable admission and `completed_processing` waits for terminal processing.

## External limits

Effectus cannot make an arbitrary external API exactly once. A timeout after a
destination may have committed is an unknown outcome. Destinations must atomically
enforce the supplied idempotency key or fencing token with their business write.

Compensation is a new external action, not a database rollback. It can fail and
cannot guarantee semantic inversion.

Kafka DLQ publication and source-offset commit are separate operations. A crash
between them can duplicate a DLQ record. Kafka offsets, PostgreSQL state, and
external effects are not one atomic transaction.

The runtime does not guarantee termination of external calls or arbitrary
embedded Go code. It does not provide mutable rule management, extension
loading, reload, rollback, server reflection, or generic external exactly-once
claims.

## Recovery

Recovery resolves the persisted generation artifact and replays the durable
outbox. It preserves stable execution, saga, dispatch, and idempotency
identities. An unknown external outcome is recorded for operator resolution;
the runtime does not assume failure and automatically compensate it.
