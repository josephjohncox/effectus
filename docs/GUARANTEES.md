# Runtime Guarantees and Design Boundaries

This document describes the guarantees in the current implementation. It also identifies planned behavior that the runtime does not provide.

Use this document as the contract for production decisions. Treat broader claims in older design papers as design goals.

## Status terms

The documentation uses these terms:

- **Implemented**: Tests cover the behavior in the main runtime.
- **Fail closed**: The runtime returns an error instead of running an incomplete path.
- **Experimental**: The path exists, but production evidence is incomplete.
- **Planned**: The repository contains a design or placeholder only.

## Supported execution paths

The checked extension runtime supports canonical `ir.Checked` workflows through `ExecuteWorkflowWithIdentity`.
A caller must configure an `OutboxStore` before execution. Each step is committed as a durable dispatch before invocation.

The generated gRPC service sends execution requests to the shared checked engine.
Each request pins the ruleset version and generation digest during durable admission.
The server does not accept mutable method registrations.

The `effectusd` list and flow bundle executor is a legacy callback-based compatibility path. It is not a checked IR execution path. The daemon rejects it unless the operator sets `--allow-legacy-execution`. The daemon also rejects `--saga` because that legacy path does not use the V2 outbox.

The following paths fail closed:

- Parallel checked workflows and non-fail-fast checked error policies.
- JSON manifests that contain a `workflows` field. Use `.eff` or `.effx` source files.
- Verbs that have no executable implementation.

A fail-closed path does not provide partial service. It returns a configuration or execution error.

## Remediation limits

The repository does not yet contain a canonical lowering pass from daemon `list.Spec` and `flow.Spec` values to `ir.Checked`. Those values contain Go continuations. For this reason, `effectusd` requires `--allow-legacy-execution` and rejects `--saga` instead of claiming V2 durability.

The inbound gRPC protocol accepts `google.protobuf.Struct` facts and uses the generated execution service.
Descriptor-driven protobuf calls remain an outbound verb executor feature.

The checked extension runtime stages immutable loader output before compilation.
It rejects JSON manifests that contain workflows. Use `.eff` or `.effx` files for ordered workflows.

Kafka DLQ publication and source-offset commit use separate broker operations. The durable ledger deduplicates records after a poison acknowledgement, but process death between first DLQ publication and acknowledgement can still duplicate the DLQ record. A Kafka transactional producer is required to close that final window.

## Checked and unchecked boundaries

Effectus provides a canonical protobuf-backed checked IR.
Some legacy compiler and execution APIs still use compatibility structures.

The daemon compiles rule sources before it publishes a compatibility generation. This compilation produces legacy `list.Spec` and `flow.Spec` values, including Go continuations. It does not produce `ir.Checked`. CLI parse and type-check commands return an error when any input fails.

Some library APIs can still construct programs directly with Go values and continuation functions. These APIs can bypass source-level checks.

Production callers that require checked semantics must use `runtime.ExecutionRuntime` with checked workflows and a configured durable outbox. Do not treat daemon compatibility execution or direct Go program construction as proof of type safety.

The target design has two explicit API classes:

1. A checked API accepts only validated IR.
2. An unchecked API accepts program builders for tests and embedding.
3. The unchecked API name must state that it bypasses validation.
4. All production entry points must require checked IR.

## Saga identity

A saga has a stable `saga_id`. Each effect occurrence has a stable `effect_id` and source-order sequence.

The current effect ID format is:

```text
step-000001
step-000002
step-000003
```

A status update uses `(saga_id, effect_id)`. It does not use the verb name. Repeated calls to the same verb therefore remain separate effect occurrences.

The store records these fields:

- Effect ID.
- Sequence.
- Verb.
- Arguments.
- Forward result.
- Status.
- Error text.
- Timestamp.

## Saga state machine

An effect uses these states:

```text
pending -> success -> compensated
pending -> failed
```

A repeated transition to the same terminal state is idempotent. Other transitions return an error.

Forward execution follows source order. Compensation follows reverse successful-execution order.

The runtime returns compensation failures to the caller. It does not only write them to a log.

## Saga replay semantics

A replay with the same saga ID and effect IDs reads stored successful results. It does not run those successful effects again.

A pending effect is different. The runtime retries a pending effect because it cannot know if the external action completed.

This crash window exists:

```text
external action succeeds
process stops before MarkSuccess
stored state remains pending
recovery retries the action
```

The current pending-effect guarantee is **at least once**. It is not exactly once.

A verb must use an external idempotency key to close this window. A future invocation protocol must pass `(saga_id, effect_id, attempt)` to every external verb.

A durable outbox can close database-local publish windows. It cannot make an arbitrary external API exactly once without API support.

## Compensation limits

A compensating verb is a new external action. It is not a database rollback.

Compensation can fail. It can also produce a state that is observably different from the original state.

For example, a refund does not erase the payment event. It creates a second event.

The runtime guarantees compensation order and error reporting. It does not guarantee semantic inversion for every verb.

Verb owners must define and test these properties:

- The inverse arguments are sufficient.
- Repeated compensation is safe.
- A partial inverse has a documented result.
- Manual recovery data is available after failure.

Nested sagas are not implemented.

## Ingestion and acknowledgement

The HTTP fact endpoint uses a bounded in-memory execution queue.

When the queue is full, the server returns HTTP 503 and a `Retry-After` header. It does not acknowledge work that it cannot queue.

The file fact store persists merged facts before queue admission. The client must retry a 503 response to request execution again.

The queue is not a durable broker. A process stop can lose queued work.

Production deployments that require durable delivery need a durable inbox or broker consumer with explicit offsets and idempotency keys.

The Kafka consumer uses consumer groups and synchronous offset commits.
It processes one application-level record at a time.
It commits only after the selected handler contract succeeds.

The daemon currently selects `completed_processing`.
A crash before the offset commit causes redelivery.
Kafka offsets are not atomic with Effectus state or external effects.
The stable Kafka delivery ID supports replay and idempotency.

The adapter also supports `durable_acceptance` for an injected checked engine handler.
The daemon rejects that mode until its fact admission transaction is durable.

## Reload generations

The daemon publishes a bundle, execution type system, and verb registry as one runtime generation.

A reload follows these steps:

1. Build a candidate outside the active generation.
2. Validate the candidate.
3. Clone mutable execution specifications.
4. Publish the complete generation under one lock.
5. Keep the previous generation if any step fails.

An execution uses one generation snapshot. A registry reload does not mutate the bundle used by an execution in progress.

This design prevents mixed bundle and type-system reads. It does not preserve in-flight external transactions across a process stop.

## PostgreSQL lifecycle storage

`PostgresStorage` is the canonical PostgreSQL rule storage implementation.

It uses migrations and sqlc-generated queries. Deployment activation and rollback use database transactions.

The legacy `PostgresRuleStorage` constructor fails closed. That backend used a second schema and did not implement the full storage interface.

The lifecycle model allows one active deployment for each ruleset name and environment. Activation deactivates older versions of the same ruleset only.

## Termination

The source rule language intends to use a finite first-order core. That core can support a structural termination argument.

The current Go flow API contains unrestricted continuation functions. An embedded continuation can compute indefinitely or create an unbounded program.

Effectus therefore does not provide an unconditional termination guarantee for all current APIs.

A valid termination theorem must apply only to checked first-order IR. It must exclude external verb duration and unrestricted host-language callbacks.

## Determinism

The runtime preserves source effect order unless an explicit future IR operation states parallel behavior.

Deterministic rule evaluation still depends on these inputs:

- Facts.
- Registered pure functions.
- Time configuration.
- External verb results.
- Store replay data.

External effects are not deterministic by default. Fixed time does not make network calls deterministic.

## Formalization plan

The formal work has three separate targets.

### First-order rule core

Define a checked IR without host-language continuations. Prove progress and preservation for that IR.

The proof must state all assumptions about registered functions and verbs.

### Invocation protocol

Define the external verb request with these fields:

```text
saga_id
effect_id
direction
attempt
verb
contract_hash
arguments
argument_hash
idempotency_key
fencing_grants
deadline
```

Define success, retryable failure, permanent failure, unknown outcome, and stale fence as separate results.

The V2 implementation is in `invocation/` and the `schema` outbox store.
See `DURABLE_SAGA_PROTOCOL.md` for the external destination contract.

### Saga and reload models

Model saga recovery and generation publication in TLA+ or an equivalent state-machine tool.

Check these properties:

- A successful effect occurrence has one stable identity.
- Compensation order is the reverse of successful forward order.
- A failed reload never replaces the active generation.
- One execution never mixes two generations.
- A pending external outcome can cause a retry.
- No model claim labels that retry window as exactly once.

## Production claim policy

Do not claim these properties without additional implementation and evidence:

- Generic exactly-once external effects.
- Unconditional termination.
- Automatic semantic rollback.
- Inbound gRPC streaming execution or server reflection.
- Atomic Kafka offsets, Effectus state, and external effects.
- Complete correspondence between the theory documents and every runtime API.
