# Verb Extension Model

This page models how a new bundle can add typed contracts and executor descriptors.
It is an abstract composition model, not a daemon hot-reload or candidate-activation API.
The daemon compiles its active generation at startup. A changed bundle requires process replacement.

Read [Runtime Lifecycle](../LIFECYCLE.md) for current behavior and [Extension System](../EXTENSION_SYSTEM.md) for bundle boundaries.

## Contract

A verb contract has this abstract form:

```math
v : (\tau_1, \tau_2, \ldots, \tau_n) \rightarrow \rho
```

The contract also contains required arguments, capability metadata, resources, and an optional inverse verb.

The declaration describes an operation. It does not prove the executor implementation satisfies the contract.

## Environment extension

Let $V$ be one bundle's map of verb names to contracts. A changed bundle proposes $V'$.

Bundle validation rejects duplicate or invalid definitions before generation construction.
This notation does not imply an automatic compatibility policy between deployed bundles.

The environment digest changes when a relevant contract changes. Checked artifacts include contract hashes for their steps.

## Compilation

For each invocation, the compiler checks:

- The verb exists.
- Argument names are unique.
- Required arguments are present.
- Argument values have compatible types.
- A result binding uses the declared result type.

Generation construction separately resolves supported executor descriptors.
For a changed contract, compile source rules against the changed bundle's environment before replacing the process.

## Interpretation

An executor interprets a checked invocation:

```math
\mathrm{execute}_v : (Args_v, Metadata, W) \rightarrow (Result_v, Outcome, W')
```

The contract declares $\rho$ as the expected result type.
That declaration alone does not prove executor conformance or successful destination commit.

$W$ represents external state. The runtime does not assume that this function is pure or deterministic.

## Supported production targets

Production effectusd supports the checked HTTP executor target. HTTP targets apply host, redirect, DNS, and response-size controls. gRPC, stream, Kafka, and OCI resolver descriptors are not supported production executor targets.

In-process Go plugins are rejected by the production daemon.

## Embedded boundary

The current embedded entry point accepts checked bundles and durable descriptors, not anonymous Go continuations.
A business service can implement Go behavior behind `executorhttp` and an HTTP descriptor.
An executor interface implementation does not become serializable IR or acquire process isolation merely by implementing the interface.
See [the Go API guide](../go-api.md) for supported and low-level entry points.

## Composition

Two verbs compose in a flow when an earlier result type matches a later argument type.

This is typed data dependency, not proof that the external operations commute or form a category.

Capability and resource declarations provide additional conflict metadata. They do not prove semantic independence.

## Versioning

A generation pins exact verb contracts and executor descriptors.
Existing executions retain their persisted generation artifact after process replacement.
Recovery resolves that historical artifact rather than rebinding unfinished work to the replacement's active generation.

A changed contract belongs to a new bundle and must pass compilation before the replacement serves new admissions.

## Security obligations

The operator must define:

- OCI trust policy
- Destination authentication
- Network policy
- Secret distribution
- Idempotency enforcement
- Fencing enforcement

The extension manifest cannot enforce these controls by itself.
