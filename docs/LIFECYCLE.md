# Runtime Lifecycle

This document defines the lifecycle terms for Effectus. Use these terms in code, APIs, logs, and operator documents.

## Terms

A **candidate** is a complete runtime change that is not active. Effectus checks a candidate before activation.

A **release** is an immutable bundle. A release has a name, version, and content digest.

An **activation** maps an environment to a release. An activation is durable control-plane state.

A **generation** is the exact snapshot that one daemon process executes. It contains one bundle, schema, verb registry, and execution configuration.

A **refresh** reads desired inputs and prepares a candidate generation. A refresh does not change the active generation by itself.

A **rollback** activates a prior release or generation. A rollback does not delete or rewrite history.

## Runtime generation rules

The daemon applies these rules:

1. One generation is active at a time.
2. Each generation has a monotonically increasing process-local ID.
3. Each generation records a digest of its serializable bundle content.
4. One execution uses one generation for its complete lifetime.
5. The daemon checks a candidate before activation.
6. A failed candidate never replaces the active generation.
7. Activation names the expected active generation.
8. The daemon rejects a stale activation with a generation conflict.
9. Bundle, schema, and verb refreshes use the same generation publication rule.
10. A rollback compiles the saved source against the current schema and verbs.
11. The status API returns the active generation ID and bundle digest.

Generation IDs are process-local. They restart when the daemon restarts. Do not use them as durable release identities.

## Build and activation flow

Use this sequence for each runtime change:

1. Capture the active generation.
2. Read all candidate inputs.
3. Parse and type-check each source once.
4. Compile the checked syntax tree.
5. Resolve the schema and verb dependencies.
6. Run the requested candidate checks.
7. Activate against the captured generation ID.
8. Reject the candidate if the generation ID changed.
9. Record history after a successful activation.

Do not publish a candidate before a health or canary check completes.

## Persistent ruleset state

The PostgreSQL storage API records ruleset releases and environment activations. It does not control a running daemon.

`DeployRuleset` performs an atomic metadata activation. The only implemented strategy is `atomic`.

The storage API rejects canary, rolling, and blue-green strategy names. No traffic controller implements these strategies.

The storage API prevents deletion of an active release. Activate another version before deletion.

A future reconciler can connect durable activations to daemon generations. Until then, treat storage activation and daemon activation as separate operations.

## Process phases

The daemon uses these phases:

```text
starting -> running -> draining -> stopped
```

Readiness is true only during the `running` phase. Shutdown changes the phase to `draining` before it stops HTTP admission.

The daemon then stops refresh workers and HTTP listeners. It allows checked recovery workers to stop within the shutdown deadline.

## Fact admission

Production effectusd requires `Idempotency-Key` and sends HTTP facts through the checked engine with `WaitAccepted`. HTTP 202 means PostgreSQL durably admitted the execution. A matching retry returns the same execution identity; a changed payload for that identity returns HTTP 409.

The local fact store is a projection. A projection failure can follow durable admission, so it does not revoke the accepted execution. Retry the same logical request with the same key and payload.

### Embedded compatibility queue

An embedded `serverState` without a checked engine retains the old process-local queue for compatibility tests. In that mode only, queue saturation returns HTTP 503 and a hard crash can lose queued work. This is not the effectusd production boundary.

## Supported HTTP behavior

The rule validation endpoint returns:

- HTTP 200 for a valid candidate.
- HTTP 422 for an invalid candidate.

The rule activation endpoint returns:

- HTTP 200 for a successful activation.
- HTTP 409 for a stale generation.
- HTTP 422 for an invalid candidate.

The rollback endpoint recompiles the saved source. It returns HTTP 422 when current dependencies reject that source.

## Current limits

The current history store is process-local for executable flow state. It is not a durable rollback log.

The persistent deployment API still stores some environment data with ruleset rows. A later migration must separate releases from activations.

The embedded compatibility queue is process-local. Production effectusd does not use it as the HTTP acknowledgement boundary.
