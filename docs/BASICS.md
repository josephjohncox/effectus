# Effectus Basics

Effectus evaluates typed facts and selects checked effect plans.

## Facts

Facts are a typed snapshot of domain data. Rules read values through paths such as `order.id` or `customer.tier`.

The compiler checks each referenced path against the candidate fact schema. The runtime pins the admitted fact payload to one execution identity.

A production request uses a namespace and a universe. The namespace separates tenants. The universe identifies a fact snapshot within that namespace.

## Verbs

A verb declares an external operation contract:

- A unique name
- Required and optional arguments
- Argument types
- A result type
- Capability and resource metadata
- An optional inverse verb
- A supported executor target

The contract says what the operation accepts. The executor defines how the operation runs.

## Effects

An effect is one occurrence of a verb with checked arguments.

Each effect occurrence receives a stable identity and source sequence. Repeated uses of the same verb remain distinct effects.

Effectus records dispatch intent before invocation. The external destination remains responsible for its own transaction and deduplication behavior.

## List rules

A `.eff` rule selects an ordered list of effects:

```eff
rule "HighRiskLargeTxn" priority 10 {
  when {
    transaction.amount > 1000
    customer.risk_score >= 80
  }
  then {
    FlagFraud(orderId: transaction.id)
    NotifyAnalyst(orderId: transaction.id)
  }
}
```

All predicates must return `bool`. The selected effects keep their source order.

## Flow rules

A `.effx` flow can bind one step result and use it in a later step:

```effx
flow "CaseHold" priority 5 {
  when {
    order.amount > 1000
  }
  steps {
    caseId = OpenCase(orderId: order.id, reason: "risk")
    UpdateCase(caseId: $caseId, status: "held")
  }
}
```

The compiler gives each result a slot. A step can reference only an earlier slot.

An undefined variable or incompatible result type causes compilation to fail.

## Predicates

Predicates compare checked values from facts, literals, and supported pure functions.

The checked IR keeps each value source in a distinct protobuf variant. The runtime does not evaluate arbitrary Go functions as production predicates.

Rule selection uses stable priority and source order. External verb results can still depend on remote state.

## Types

Effectus supports primitive, list, object, and named types through its declaration environment.

The compiler checks:

- Fact paths
- Literal compatibility
- Verb arguments
- Required arguments
- Result bindings
- Predicate result types

A successful type check does not prove that an external service honors the declared contract.

## Capabilities

Capability metadata describes access and conflict requirements. A verb can declare read, write, create, delete, exclusive, commutative, idempotent, and related flags.

The runtime uses the strongest applicable requirement when it protects a resource. A local lock provides process-local coordination only.

Durable fencing requires a monotonic provider and destination enforcement.

## Compensation

A verb can declare an inverse operation. The runtime records each successful effect and compensates in reverse source order after a later failure.

An inverse is not a mathematical inverse unless the verb owner makes it one. Compensation can also fail.

The runtime does not compensate an effect with an unknown external outcome.

## Checked and compatibility paths

Production effectusd compiles source into checked first-order IR. This representation contains no Go callbacks.

Embedded applications can still use legacy list specifications and flow continuations. Those compatibility values are outside the production guarantees.

## Generations

A generation contains the bundle, declaration environment, executors, checked artifacts, and digests.

The runtime publishes a validated generation atomically. Each admitted execution stays pinned to its original generation.

## Next steps

- Use [Tutorials](TUTORIALS.md) for short examples.
- Read [Architecture](ARCHITECTURE.md) for the production data path.
- Read [Runtime Guarantees](GUARANTEES.md) before deployment.
- Read [Extension System](EXTENSION_SYSTEM.md) to define executors.
