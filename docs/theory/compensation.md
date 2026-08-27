# Compensation Model

This document models durable forward execution and reverse compensation.

Read [Durable Saga Protocol](../DURABLE_SAGA_PROTOCOL.md) for the normative state machine.

## Effect occurrences

A workflow plan contains ordered effect occurrences:

```math
\pi = [e_0, e_1, \ldots, e_{n-1}]
```

Each occurrence has its own stable identity and sequence number. Two occurrences of the same verb are not the same effect.

An effect can declare an inverse operation $c_i$. The inverse is another external verb invocation.

## Durable dispatch

Before an invocation, the runtime records a dispatch intent:

```math
d_i = (id_i, sequence_i, attempt_i, lease_i, token_i, state_i)
```

The dispatch state records whether the outcome is pending, successful, retryable, permanent, unknown, or blocked.

A worker can complete a dispatch only with its current lease owner and fencing token.

## Forward transition

For the next effect $e_i$, the runtime performs this abstract sequence:

1. Persist the dispatch intent.
2. Get or renew a lease.
3. Invoke the external destination.
4. Validate the returned result.
5. Persist the classified outcome.

A process can stop between invocation and outcome persistence. The runtime then cannot know whether the destination committed the operation.

## Unknown outcome

An unknown outcome is not a normal retryable failure.

The runtime records `blocked_unknown` and stops automatic compensation for the affected dependency chain.

An operator or destination-specific reconciliation process must resolve the ambiguity.

## Reverse order

If a later forward effect fails with a known outcome, the runtime considers recorded successful effects in reverse source order:

```math
[e_0, e_1, \ldots, e_k]
\mapsto
[c_k, c_{k-1}, \ldots, c_0]
```

Each compensation receives its own durable dispatch state. A compensation can fail or become blocked.

## Semantic inverse obligation

The runtime does not prove that $c_i$ reverses $e_i$.

The verb owner must define the relevant equivalence relation and maintain this law:

```math
\mathrm{invoke}(c_i, \mathrm{invoke}(e_i, W)) \approx W
```

The relation $\approx$ can ignore approved observations, such as audit records. It must not hide business state that the caller expects to restore.

## Idempotency obligation

Recovery can invoke a pending dispatch more than once. The destination must enforce the stable idempotency key when duplicate application is unsafe.

A recorded successful result replays without another invocation.

## Fencing obligation

A durable monotonic token orders Effectus workers. End-to-end fencing requires the destination to reject a stale token.

A local advisory token cannot fence another process.

## Unsupported composition

Nested saga transactions are not supported. The runtime rejects them instead of ignoring an inner boundary.

Parallel saga branches are also outside this sequential compensation model.

## Model properties

Under durable-store and destination assumptions, the model targets these properties:

- A recorded success has one stable effect identity.
- Recovery keeps source order for forward dependencies.
- Compensation considers successful effects in reverse source order.
- A stale lease token cannot complete a durable dispatch.
- An unknown outcome does not trigger automatic compensation.

The TLA+ saga model checks bounded instances of these properties. It does not prove destination idempotency or semantic inversion.
