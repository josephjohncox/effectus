# Effectus Architecture

This document describes the production architecture. Read [Runtime Guarantees](GUARANTEES.md) for the exact contract and limits.

## System boundary

Effectus compiles typed rule sources, admits fact snapshots, selects checked plans, and records execution state.

External verb destinations remain separate systems. They control their own transactions, availability, and deduplication behavior.

```text
Rule sources and contracts
          |
          v
  compiler.CompileChecked
          |
          v
   checked protobuf IR
          |
          v
 immutable generation  <----- schema and verb refresh
          |
          v
 HTTP / Kafka / generated gRPC / recovery
          |
          v
     runtime.Engine
          |
          +----> execution ledger
          +----> saga outbox
          +----> verb executors
```

## Compile path

The production compile path has four stages:

1. Load `.eff` and `.effx` source files.
2. Build an immutable environment from fact types, functions, and verb contracts.
3. Compile the sources with `compiler.CompileChecked`.
4. validate and serialize the artifact through the `ir` package.

The checked artifact contains no Go callbacks. It uses the protobuf schema in `effectus/v1/ir.proto`.

The checker validates fact paths, argument types, result slots, plan order, contract hashes, and structural limits. It rejects unknown protobuf fields.

Read [Checked IR](../ir/README.md) for the full checker contract.

## Runtime generations

A runtime generation contains a coherent snapshot of:

- The source bundle
- Fact schemas and functions
- Verb contracts and executors
- Checked rule artifacts
- Content and environment digests

Activation uses an expected-generation comparison. A stale candidate cannot replace a newer active generation.

Each accepted execution records its generation. A later activation does not change that execution.

Schema and verb refreshes recompile the rule sources before publication. A failed candidate never becomes active.

Read [Runtime Lifecycle](LIFECYCLE.md) for activation, refresh, drain, and shutdown rules.

## Execution path

All production transports call `runtime.Engine.Execute`.

The engine performs these steps:

1. Validate and normalize the admission request.
2. Derive or validate stable admission and execution identities.
3. write the admission and payload hash to the execution ledger.
4. pin the active generation.
5. select checked plans from the admitted facts.
6. record execution and dispatch state.
7. run or recover each plan through the shared workflow runtime.

A repeated admission with the same identity and payload converges on the existing execution. The engine rejects the same identity with a different payload.

## Durable state

PostgreSQL stores the production execution ledger and V2 saga outbox.

The ledger records:

- Admission identity and payload hash
- Ruleset, version, and generation
- Selected plan identities
- Applied facts
- Execution status and recovery lease

The saga outbox records:

- Stable effect and dispatch identities
- Source sequence
- Forward and compensation arguments
- Attempts and outcomes
- Lease owner, expiry, and fencing token
- Result or blocked-state details

The runtime writes dispatch intent before it invokes an external verb. A worker must hold the current lease token before it completes a dispatch.

Read [Durable Saga Protocol](DURABLE_SAGA_PROTOCOL.md) for the state machine and recovery rules.

## Transport adapters

### HTTP

The HTTP source validates authentication and body limits before admission. A full internal queue returns HTTP 503.

A successful response means the selected acknowledgement boundary completed. It does not prove an external effect occurred exactly once.

### Kafka

Kafka uses consumer groups and stable delivery identities. It commits an offset only after durable acceptance or completed processing.

The selected acknowledgement contract controls that boundary. Poison handling supports halt, skip, and non-transactional DLQ policies.

DLQ publication and source-offset commit are separate operations. A process stop between them can duplicate a DLQ record.

### gRPC

Effectusd registers the generated `effectus.v1.RulesetExecutionService` before it starts the server.

The service applies authentication, transport limits, deadlines, typed facts, and generation pinning. Management RPCs return `Unimplemented`.

The deprecated `runtime/ruleset_execution.proto` remains a schema-compatibility artifact. Effectusd does not register that service.

## Verb execution

A checked plan can invoke supported HTTP, gRPC, stream, Kafka, or OCI-resolved executors.

Each invocation carries stable identity, attempt, contract, and fencing metadata. The destination must enforce the metadata when correctness depends on it.

In-process Go continuations and plugins are compatibility-only paths. They are not valid production checked IR.

## Capability and fencing model

Capabilities describe access and conflict properties. They help the runtime select a conservative lock strategy.

A process-local lock and token provide advisory protection inside one process. They do not fence another process.

The PostgreSQL fencing provider issues durable monotonic tokens. An external destination must reject stale tokens for end-to-end fencing.

## Failure model

Effectus distinguishes these outcomes:

- Success with a recorded result
- Retryable failure
- Permanent failure
- Unknown external outcome
- Dependency block
- Fence block

The runtime does not compensate an operation with an unknown outcome. It records `blocked_unknown` for operator resolution.

Compensation runs recorded inverse operations in reverse source order. The runtime records every compensation failure.

## Process lifecycle

Effectusd moves through these phases:

```text
starting -> running -> draining -> stopped
```

Readiness requires a running phase, an active generation, and healthy required dependencies.

Shutdown stops new admissions first. It then drains accepted work within the configured deadline.

## Deployment model

The Helm chart supports immutable images, digest-pinned bundles, Secrets, TLS ports, probes, persistent storage, and graceful termination.

Production OCI loading requires a digest and an operator-provided signature verifier. The verifier defines the trust policy.

Read [Production Runbook](PRODUCTION_RUNBOOK.md) before deployment.
