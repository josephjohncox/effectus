# Runtime Guarantees and Limits

This document states current guarantees only.

## Implemented guarantees

- `effectusc check`, `compile`, and `inspect` consume immutable SourceBundle
  documents. Checking rejects invalid rule and declaration inputs.
- `effectusd` compiles one active generation at startup for new admissions.
  Existing executions retain their pinned artifacts. The engine can resolve historical generations for replay and recovery.
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

Effectus cannot make an arbitrary external API exactly once.
A timeout after possible destination commit is an unknown outcome.
To prevent duplicate business effects, destinations must atomically coordinate idempotency records with their business writes.
Destinations must also enforce fencing when stale owners could perform unsafe writes.
Fencing rejects stale authority. It does not deduplicate repeated operations under valid authority.

Compensation is a new external action, not a database rollback. It can fail and
cannot guarantee semantic inversion.

Kafka DLQ publication and source-offset commit are separate operations. A crash
between them can duplicate a DLQ record. Kafka offsets, PostgreSQL state, and
external effects are not one atomic transaction.

The runtime does not guarantee termination of external calls or arbitrary
embedded Go code. It does not provide mutable rule management, extension
loading, reload, rollback, server reflection, or generic external exactly-once
claims.

## Shutdown

Shutdown stops admission and cancels intake workers, then drains and joins entered handlers and workers.
Only then does the daemon close the engine, its owned generation resources, and the borrowed database, in that order.
Drain deadlines initiate cancellation. Non-cooperative callbacks can keep shutdown waiting beyond those deadlines.
The daemon does not promise to finish every durable accepted execution before exit.

## Recovery

Recovery resolves the persisted generation artifact and replays the durable
outbox. It preserves stable execution, saga, dispatch, and idempotency
identities. Unknown external outcomes block by default.
A checked `SINK_GUARANTEED` step can retry a valid unknown outcome within its checked attempt budget, using those same identities.
`KEY_REQUIRED` alone does not permit unknown-outcome retry.
Unauthorized or exhausted unknown outcomes remain blocked for operator investigation.
The runtime does not assume failure and automatically compensate them.

The destination must actually coordinate deduplication with its business commit.
Declaring a policy or sending a key does not establish that guarantee.
See [unknown-outcome handling](DURABLE_SAGA_PROTOCOL.md#unknown-outcomes).
