# Notation and Proof Obligations

This appendix defines compact notation for the theory notes.

It gives proof sketches for the abstract model only. It does not prove correspondence with all Go code.

## Syntax

Use these forms for checked values:

```math
v ::= literal(a) \mid fact(p) \mid result(i)
```

Use this form for one checked step:

```math
s ::= invoke(verb, args, slot?)
```

A plan is a finite list:

```math
\pi ::= [] \mid s :: \pi
```

## Well-formed plans

A plan is well formed when:

1. Step ordinals start at zero and have no gaps.
2. Result slots start at zero and have no gaps.
3. Each result reference points to an earlier slot.
4. Each argument matches the declared verb contract.
5. Each contract hash matches the environment.

`ir.Check` enforces these conditions and additional structural limits.

## Value evaluation

Let $f$ be facts and $R$ be recorded prior results:

```math
\begin{aligned}
\langle literal(a), f, R \rangle &\Downarrow a \\
\langle fact(p), f, R \rangle &\Downarrow lookup(f, p) \\
\langle result(i), f, R \rangle &\Downarrow R(i)
\end{aligned}
```

Well-formedness guarantees that $R(i)$ refers to an earlier declared slot. Runtime state must still contain the recorded result.

## Plan transition

Let an internal plan state be $(\pi, k, R, D)$.

Preparing a step records durable dispatch intent:

```math
(\pi, k, R, D) \rightarrow (\pi, k, R, D[k \mapsto pending])
```

A known successful outcome advances the ordinal:

```math
(\pi, k, R, D[k \mapsto success(r)])
\rightarrow
(\pi, k+1, R[slot(k) \mapsto r], D)
```

A retryable outcome updates attempt state without changing source order.

An unknown outcome moves the execution to a blocked state.

## Compensation transition

Let $C$ contain successful compensatable steps in source order.

```math
reverse(C) = [c_k, c_{k-1}, \ldots, c_0]
```

The runtime gives each compensation its own durable dispatch record. A compensation result does not erase the forward audit record.

## Finite traversal lemma

**Model property:** A well-formed checked plan has finite internal traversal when each step reaches a classified outcome in finite time.

**Reason:** The plan has finite length. A successful transition increases $k$. Retry policy has a finite attempt bound.

This property excludes an external call that never reaches a classified outcome. It also excludes indefinite operator delay in a blocked state.

## Result-reference lemma

**Model property:** Evaluation never reads a result slot from a future step.

**Reason:** The checker accepts `result(i)` only when an earlier step defines slot $i$.

This property depends on correct correspondence between the checked artifact and runtime result map.

## Stable-order lemma

**Model property:** A fixed checked artifact presents steps in one stable order.

**Reason:** Step ordinals are unique and contiguous. The runtime traverses them in ordinal order.

This does not make external outcomes deterministic.

## Generation-pinning property

**Model property:** Publication of generation $g+1$ does not change an execution admitted under generation $g$.

The execution ledger records the generation. Recovery loads the same generation artifact.

The TLA+ generation model checks bounded publication and pinning transitions.

## Stale-completion property

**Model property:** A stale owner or fencing token cannot complete a durable dispatch record.

The durable store compares owner and token in the completion update.

This protects internal state only. Destination-side fencing remains a separate proof obligation.

## Correspondence obligations

A full implementation proof must connect:

- Source syntax to compiler output
- Compiler output to protobuf IR
- `ir.Check` to the well-formedness rules
- Engine transitions to execution-ledger updates
- Workflow transitions to saga-outbox updates
- Store transactions to database isolation behavior
- Invocation metadata to destination enforcement

Repository tests and bounded models cover parts of this list. They do not form one machine-checked correspondence proof.
