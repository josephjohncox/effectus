# Checked Compilation Flow

This document maps rule and extension inputs to the production execution engine.

## Inputs

A candidate generation can contain:

- `.eff` list rules
- `.effx` flow rules
- Fact type declarations
- Function declarations
- Verb contracts
- Supported executor configuration

Inputs can come from a local bundle, an extension directory, or a signed OCI bundle.

Production OCI references use digests. Effectusd does not poll mutable tags.

## Build the environment

The loader resolves declarations before rule compilation. It rejects duplicate or incompatible definitions according to the configured policy.

The compiler builds an immutable environment with:

- Fact paths and types
- Pure predicate functions
- Verb argument and result contracts
- Capability and resource declarations

The environment digest identifies this declaration set.

## Compile source

`compiler.CompileChecked` parses and checks each rule source.

For list rules, it preserves source effect order. For flow rules, it assigns result slots in step order.

The compiler rejects:

- Unknown fact paths
- Unknown verbs or functions
- Invalid predicate types
- Missing or duplicate arguments
- Incompatible literals and result bindings
- References to future result slots
- Unsupported nested saga boundaries

## Check the artifact

The `ir` package validates the protobuf artifact again before execution or storage.

The checker applies structural limits and recalculates environment and contract hashes. It rejects unknown protobuf fields.

`Checked.Marshal` produces deterministic bytes. `Checked.Digest` identifies the exact artifact content.

Read [Checked IR](https://github.com/josephjohncox/effectus/blob/main/ir/README.md) for the full checker list.

## Build a candidate generation

The runtime combines the checked artifacts with the exact schemas, verb contracts, executors, and bundle manifest.

It validates the complete candidate before publication. A failed candidate releases its resources and leaves the active generation unchanged.

## Publish atomically

Activation compares the candidate base generation with the current active generation.

If they match, the runtime publishes the candidate as one immutable snapshot. If they do not match, activation returns a generation conflict.

A schema or verb refresh recompiles existing source rules against the candidate declarations before publication.

## Admit work

HTTP, Kafka, generated gRPC, and recovery use `runtime.Engine.Execute`.

The engine records the admission identity, payload hash, ruleset, version, and generation. It then records the selected checked plans.

A duplicate identity with the same payload returns the existing execution. A duplicate identity with different facts fails.

## Execute and recover

The workflow runtime records each dispatch intent before invocation. A worker completes a dispatch only while it holds the current lease token.

Recovery gets a new lease and uses the same execution, plan, effect, and dispatch identities.

Completed results replay from durable state. Unknown external outcomes enter a blocked state for operator action.

## Refresh and drain

A successful refresh affects new admissions only. Existing executions keep their pinned generation.

During shutdown, effectusd stops admission and drains accepted work. It then retires unused generation resources.

Read [Runtime Lifecycle](LIFECYCLE.md) for the complete state machine.

## Compatibility paths

Embedded Go applications can use legacy specifications and continuations. These values contain process-local behavior and cannot form checked artifacts.

Production effectusd rejects legacy in-memory specifications and in-process plugins.
