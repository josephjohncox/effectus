# Core Semantic Model

This document gives an abstract model for checked Effectus execution.

It does not model transport code, storage implementation details, or unrestricted Go continuations.

## Declarations

Let an environment contain:

```math
\Gamma = (F, V, P)
```

Where:

- $F$ maps fact paths to types.
- $V$ maps verb names to argument and result contracts.
- $P$ maps pure function names to checked signatures.

The environment is immutable within one runtime generation.

## Facts

A fact payload is a finite typed value:

```math
f : \mathrm{Facts}_{\Gamma}
```

A path lookup succeeds only when the path exists in $F$ and the payload contains a compatible value.

Adding a field does not change a rule that never reads that field. This property assumes the existing paths keep their types and values.

## Checked values

A checked argument has one source:

```math
v ::= \mathrm{Literal}(a) \mid \mathrm{Fact}(p) \mid \mathrm{Result}(i)
```

A result reference $\mathrm{Result}(i)$ can use only a slot produced by an earlier step.

## Plans

A checked plan is a finite ordered sequence:

```math
\pi = [s_0, s_1, \ldots, s_{n-1}]
```

Each step contains a verb, checked arguments, an ordinal, and an optional result slot.

List rules compile directly to ordered steps. Flow rules compile bindings to result-slot references.

The production representation does not contain a continuation function.

## Rule selection

Let $Q$ be a finite ordered set of checked rules. Each rule has a predicate and a plan.

```math
\mathrm{select}(Q, f) = [\pi_q \mid q \in Q \land \mathrm{predicate}(q, f) = \mathrm{true}]
```

The compiler orders rules by priority and stable source order.

Selection is deterministic when the artifact, environment, facts, and pure predicate implementations are fixed.

## Internal execution state

An abstract execution state is:

```math
E = (\pi, k, R, S)
```

Where:

- $\pi$ is the selected plan.
- $k$ is the next step ordinal.
- $R$ maps completed result slots to values.
- $S$ records durable dispatch states.

An internal transition can prepare the next dispatch, record an outcome, or enter a blocked state.

## External interpretation

A verb interpreter has this abstract shape:

```math
\mathrm{invoke} : (\mathrm{Verb}, \mathrm{Args}, \mathrm{Metadata}, W) \rightarrow (\mathrm{Outcome}, W')
```

$W$ is external world state. Effectus does not control this state transition.

The result can depend on remote state even when the checked plan is fixed.

## Type property

A useful model property is preservation of checked value types:

```math
\Gamma \vdash E : \mathrm{valid} \land E \rightarrow E'
\implies \Gamma \vdash E' : \mathrm{valid}
```

This property assumes each executor returns a value compatible with its declared contract. Runtime result validation enforces that boundary.

## Progress boundary

A checked internal state can identify its next transition or terminal state.

This statement does not mean that an external call completes. A call can time out, return an unknown outcome, or remain blocked.

## List analogy

Finite effect lists form a monoid under concatenation. The empty list is the identity.

This analogy explains source-order composition. It does not prove runtime durability or external behavior.

## Legacy continuation API

The legacy Go flow API can store arbitrary continuation functions. It can express behavior that the checked first-order IR cannot serialize.

Properties of the checked plan model do not automatically apply to that API.
