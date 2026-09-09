# Effectus Design Principles

This document records the main design choices. It does not define a separate runtime contract.

Use [Runtime Guarantees](GUARANTEES.md) when you need normative behavior.

## One checked production representation

Production source files compile into the protobuf IR defined in `effectus/v1/ir.proto`.

The checked IR contains data, not Go functions. The runtime accepts an artifact only after `ir.Check` validates it against an immutable environment.

Retained compatibility declarations do not introduce another supported execution representation.
The current embedded API also accepts checked source bundles and durable descriptors, not anonymous Go continuations.

## One production execution engine

HTTP, Kafka, generated gRPC, embedded execution, and recovery call `runtime.Engine.Execute`.

This shared path prevents transport-specific admission, identity, generation, and recovery behavior.

## Immutable generations

Startup compilation builds one active generation with an immutable environment, verb contracts, resolved executors, checked artifacts, and digests.

Changing that generation requires a new source bundle and process replacement.
The daemon has no candidate-publication or in-process activation phase.
Request generation constraints guard admission and replay identity, not deployment activation.

Existing executions keep their pinned artifacts. The engine can resolve historical generations without changing its active generation.

## Durable intent before external work

The runtime records an execution and dispatch intent before it invokes an external verb.

A dispatch has a stable identity, idempotency key, attempt, lease, and fencing token. Recovery uses the same record after a process stop.

The destination controls whether the external operation is idempotent or fenced. Effectus cannot impose that behavior through metadata alone.

## Explicit outcome states

The runtime distinguishes a retryable failure from an unknown external outcome.

A retryable failure can consume another attempt. An unknown outcome blocks automatic compensation because the forward operation might have committed.

This rule prevents the runtime from claiming a rollback that it cannot prove.

## Reverse compensation

Each effect occurrence has its own identity and sequence number. Compensation uses these values instead of the verb name.

The runtime compensates recorded successes in reverse source order. It records each compensation error.

Nested saga transactions are not supported. The compiler and runtime reject them.

## Fail-closed configuration

Effectusd uses command-line flags and supported environment variables, not a general YAML runtime configuration.
Its JSON boundaries reject unknown fields, multiple values, and oversized bodies.
Startup rejects conflicting bundle modes and invalid settings before serving.

Unsupported production paths return explicit errors. The daemon does not silently select a compatibility executor.

## Conservative concurrency

Capability metadata describes access and conflict behavior. The runtime selects the strongest required capability for each protected resource.

Process-local locks provide advisory coordination only. Durable distributed fencing uses monotonic PostgreSQL tokens and destination enforcement.

## Bounded inputs

The compiler, IR parser, HTTP source, gRPC service, archive extractor, and remote executors apply explicit size and structure limits.

OCI extraction rejects traversal, links, device entries, excessive file counts, and excessive expanded sizes.

## Resource lifetime

The daemon prepares listeners before starting services and cleans up partial preparation failures.
Shutdown stops admission, cancels intake, and drains entered handlers.
It joins handlers and workers before closing the engine, then closes the borrowed database.

Cancellation is cooperative. A drain deadline triggers cancellation but does not terminate Go callbacks.
A non-cooperative callback can extend shutdown while it still uses dependencies.
The ledger retains unfinished accepted work for recovery after process replacement.

## Compatibility policy

Compatibility artifacts can remain when removal would break a published schema contract. The daemon must not register or execute them by accident.

The deprecated dynamic gRPC schema follows this policy. Generated `effectus.v1` execution is the production service.

## Formal models

The TLA+ models check bounded saga and abstract generation state machines.
Any candidate-activation transition in a model is not an implemented daemon reload feature.
These models do not prove external service behavior or full Go implementation equivalence.

Read [Theory Notes](theory/README.md) for semantic models and [Executable State Models](https://github.com/josephjohncox/effectus/blob/main/formal/README.md) for model scope.

## Related documents

- [Architecture](ARCHITECTURE.md)
- [Runtime Lifecycle](LIFECYCLE.md)
- [Durable Saga Protocol](DURABLE_SAGA_PROTOCOL.md)
- [Checked Compilation Flow](coherent_flow.md)
- [Production Runbook](PRODUCTION_RUNBOOK.md)
