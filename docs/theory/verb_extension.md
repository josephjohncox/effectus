# Verb Extension Model

A verb extension adds typed operation contracts and executor configuration to a candidate environment.

Read [Extension System](../EXTENSION_SYSTEM.md) for supported manifest formats.

## Contract

A verb contract has this abstract form:

```math
v : (\tau_1, \tau_2, \ldots, \tau_n) \rightarrow \rho
```

The contract also contains required arguments, capability metadata, resources, and an optional inverse verb.

The declaration describes an operation. It does not prove the executor implementation satisfies the contract.

## Environment extension

Let $V$ be the active map of verb names to contracts. A candidate extension proposes $V'$.

The loader applies duplicate and compatibility policy before it builds the candidate environment.

The environment digest changes when a relevant contract changes. Checked artifacts include contract hashes for their steps.

## Compilation

For each invocation, the compiler checks:

- The verb exists.
- Argument names are unique.
- Required arguments are present.
- Argument values have compatible types.
- A result binding uses the declared result type.
- The selected executor target is supported.

A schema or verb refresh recompiles existing rule sources against the candidate environment.

## Interpretation

An executor interprets a checked invocation:

```math
\mathrm{execute}_v : (Args_v, Metadata, W) \rightarrow (Result_v, Outcome, W')
```

The runtime validates the result against $\rho$ before it records successful completion.

$W$ represents external state. The runtime does not assume that this function is pure or deterministic.

## Supported production targets

Production effectusd supports checked HTTP, gRPC, stream, Kafka, and OCI-resolved targets.

OCI loading requires a digest and an operator-provided signature verifier. HTTP targets apply host, redirect, DNS, and response-size controls.

In-process Go plugins are rejected by the production daemon.

## Static embedded executors

A trusted Go application can register a static executor through the library API.

This path can contain arbitrary Go behavior. It does not become serializable checked IR and does not gain daemon process isolation.

## Composition

Two verbs compose in a flow when an earlier result type matches a later argument type.

This is typed data dependency, not proof that the external operations commute or form a category.

Capability and resource declarations provide additional conflict metadata. They do not prove semantic independence.

## Versioning

A production generation pins exact verb contracts and executors. Existing executions keep that generation after a refresh.

A changed contract must produce a new candidate and pass compilation before activation.

## Security obligations

The operator must define:

- OCI trust policy
- Destination authentication
- Network policy
- Secret distribution
- Idempotency enforcement
- Fencing enforcement

The extension manifest cannot enforce these controls by itself.
