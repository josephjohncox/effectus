# Durable saga dispatch protocol

The V2 library and `runtime.ExecutionRuntime.ExecuteWorkflowWithIdentity` store each checked workflow effect as an outbox dispatch.
The `effectusd` legacy bundle executor does not implement this protocol and rejects `--saga`.
The dispatcher does not call an external executor before it commits the dispatch.
Compensation also uses an outbox dispatch.
`EnqueueCheckedStep` derives the step ID, order, verb, and contract hash from `ir.Checked`.
It rejects a saga when its stored plan digest does not match the checked artifact, when the saga ID is not `StableSagaID(execution_id, plan_id)`, or when supplied compatibility arguments differ from values resolved from checked literals, facts, and result slots.

This protocol provides durable intent and at-least-once dispatch.
It does not provide generic exactly-once external mutation.

## Stable identity

A dispatch uses these identities:

```text
saga_id         = SHA-256(execution_id, plan_id)
effect_id       = checked IR step ID
idempotency_key = SHA-256(store namespace, saga_id, effect_id, direction)
```

The attempt number is not part of the idempotency key.
A retry uses the same idempotency key and a larger attempt number.
V2 currently rejects non-serial saga creation. This fail-closed restriction prevents a late concurrent success from escaping compensation after another forward step fails.

The store rejects a repeated identity when these values change:

- The verb name.
- The verb contract hash.
- The canonical arguments.
- The argument hash.
- The step sequence.
- The compensation contract.
- The fencing requirements.

## Dispatch sequence

The dispatcher uses this sequence:

1. Claim one committed dispatch.
2. Increase its attempt number.
3. Store a random lease token and lease deadline.
4. Acquire resource leases in canonical order.
5. Get fencing grants from the provider.
6. Store the grants with attempt and lease-token checks.
7. Call the invocation-aware executor.
8. Store the classified outcome with the same checks.

The external call occurs after step 6.
It does not occur inside a database transaction.

An expired claim can move to a new worker.
The new claim gets a new lease token and a larger attempt number.
The store rejects completion from the old worker.

## Invocation request

An invocation-aware executor receives these immutable fields:

```text
request_id
execution_id
saga_id
effect_id
direction
attempt
idempotency_key
deadline
verb
contract_hash
canonical arguments
argument_hash
fencing grants
```

System metadata does not come from caller arguments or caller headers.
An adapter must not let caller data replace an idempotency key or fencing grant.

Strict durable mode requires an invocation-aware executor.
The `CapIdempotent` flag does not meet this requirement.

The HTTP executor sends system metadata in reserved headers:

```text
Idempotency-Key
X-Effectus-Execution-ID
X-Effectus-Saga-ID
X-Effectus-Effect-ID
X-Effectus-Attempt
X-Effectus-Direction
X-Effectus-Argument-Hash
X-Effectus-Contract-Hash
X-Effectus-Fencing-Grants
X-Effectus-Deadline
```

`X-Effectus-Fencing-Grants` contains a JSON array in canonical resource order.
Static executor configuration cannot set these headers.
A non-success response uses `X-Effectus-Outcome` for explicit classification.
An absent or invalid classification becomes `unknown_outcome`.

## Outcome classes

An executor must return one outcome class:

```text
success
retryable_failure_known_not_committed
permanent_failure
unknown_outcome
stale_fence
```

A timeout after possible transmission is an `unknown_outcome`.
A connection reset after possible transmission is also an `unknown_outcome`.

Effectus retries an unknown outcome with the same idempotency key.
It does not start compensation for an unknown forward outcome.
An exhausted unknown outcome moves the saga to `blocked_unknown`.

A permanent forward failure starts durable reverse-order compensation.
A stale-fence outcome moves the saga to `blocked_fence`.
Effectus does not send stale-fence outcomes through a generic retry loop.

## External destination contract

Exactly-once business mutation needs destination support.
The destination must complete these operations atomically:

1. Deduplicate the stable idempotency key with the business mutation.
2. Reject the same key when its argument hash changes.
3. Store and replay the original terminal result.
4. Reject a fencing token below its resource watermark when fencing applies.
5. Keep the deduplication record beyond the Effectus retry period.

If the destination cannot meet this contract, duplicate business mutations remain possible.
Effectus still preserves the stable identity and durable attempt history.

## Fencing guarantees

A fencing provider reports one guarantee:

```text
local_advisory
durable_monotonic
```

The in-memory provider is `local_advisory`.
Its counter does not survive a process restart.
Do not describe its tokens as distributed fencing.

The PostgreSQL provider is `durable_monotonic` when PostgreSQL is durable.
It stores counters and leases in the V2 migration tables.
Two independent provider clients share the same token sequence.

A destination enforces fencing only when it rejects stale tokens.
Token propagation alone is not enforcement.
Use these status terms in operational output:

```text
not_requested
local_lock_only
propagated
acknowledged
stale_rejected
```

## Persistence

Apply `schema/migrations/10001_saga_outbox_v2.sql` before worker startup.
The V2 store does not run startup `ALTER TABLE` statements.
It does not write the legacy saga tables.

The migration creates these tables:

```text
effectus_saga_instances
effectus_saga_steps
effectus_saga_outbox
effectus_saga_attempts
effectus_fencing_counters
effectus_fencing_leases
```

The Redis V2 store uses one versioned state document per configured prefix.
It uses `WATCH` and `MULTI` for atomic state transitions.
It retries optimistic conflicts and uses Redis server time for lease checks.
A configured TTL applies to the complete recovery document only after every stored saga is terminal. Active recovery state is persisted without expiration.
Use a unique prefix for each independent deployment.
The state-document design favors protocol correctness over high write throughput.
Use PostgreSQL when one Redis document would create excessive contention or size.

The in-memory V2 store implements the same state checks for tests.
It does not provide durable recovery.
