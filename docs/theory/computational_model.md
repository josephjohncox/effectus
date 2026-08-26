# Computational Model: Beyond Turing Completeness

> **Status:** The termination claims apply only to the proposed checked first-order core. The current Go flow API accepts unrestricted continuation functions. See [`../GUARANTEES.md`](../GUARANTEES.md).

## 1. Introduction to Computational Bounds

Effectus deliberately adopts a computational model that is *not* Turing complete. Instead, it uses free monads (for flows) and free monoids (for lists) to represent and execute business rules. This design decision provides significant benefits for reasoning, verification, and operational safety.

```math
\begin{align}
\text{SpecList}  &= \text{Facts} \rightarrow \text{List(Effect)} \\
\text{SpecFlow}  &= \text{Facts} \rightarrow \text{Program Unit}
\end{align}
```

Both representations limit computational power in exchange for stronger guarantees about behavior, analyzability, and implementation safety.

## 2. Theoretical Foundations

### 2.1. Computational Hierarchy

In the Chomsky hierarchy of formal languages, Effectus rules deliberately occupy a constrained position:

```math
\begin{align}
\text{Regular} \subset \text{Context-Free} \subset \text{Context-Sensitive} \subset \text{Recursively Enumerable}
\end{align}
```

The list dialect uses a form of regular expressions (sequential composition of effects), while the flow dialect uses a context-free grammar (with limited recursion through the free monad).

### 2.2. Free Structures as Bounded Computation

Free algebraic structures provide precisely the computational power needed for rule execution without excess:

```math
\begin{align}
\text{Free Monoid} &= \text{Sequential Composition} \\
\text{Free Monad} &= \text{Sequential Composition} + \text{Binding}
\end{align}
```

These structures are "free" in the categorical sense — they impose exactly the laws required by their algebraic definition and no more, making them optimal for representing effect sequences.

## 3. Why Not Turing Complete?

### 3.1. The Halting Problem and Decidability

A key limitation of Turing-complete languages is the undecidability of the halting problem:

**Theorem (Rice)**: For any non-trivial property of the partial functions, it is undecidable whether a program computes a partial function with that property.

This fundamental barrier means that in Turing-complete languages, we cannot generally determine:

- Whether a program will terminate
- Resource bounds (time/space complexity)
- Freedom from unintended behaviors

### 3.2. Static Analysis Benefits

By constraining the computational model, Effectus gains critical static analysis capabilities:

```math
\begin{align}
\text{Termination} &: \text{Guaranteed} \\
\text{Resource Bounds} &: \text{Statically Determinable} \\
\text{Effect Sequence} &: \text{Analyzable}
\end{align}
```

These properties enable compile-time verification of critical business properties that would be undecidable in a Turing-complete system.

## 4. Practical Implications

### 4.1. Rule Safety Properties

The checked source language has a finite number of rules and steps. This gives the compiler a finite syntax tree to analyze.

This property does not bound external verb duration or resource use. It also does not cover direct Go continuation APIs.

Stable source order makes rule selection repeatable for the same checked artifact and facts. External verbs can still be nondeterministic.

### 4.2. Operational Advantages

These theoretical properties translate directly to operational benefits:

| Property | Operational Benefit |
| ---------- | --------------------- |
| Finite checked syntax | The compiler can inspect every source step |
| Stable rule order | Equal-priority rules keep their source order |
| Static type checks | The compiler rejects known type errors |
| Explicit effect boundary | External operations remain visible and testable |

## 5. Free Structures as Implementation Strategy

### 5.1. Free Monoids (Lists)

The list dialect uses the free monoid structure:

```math
\begin{align}
\text{List}(\text{Effect}) &\cong F^{\star}(\text{Effect}) \\
F^{\star}(X) &= 1 + X + X^2 + X^3 + \ldots
\end{align}
```

This enables:

- Simple sequential execution
- Sequential execution in source order
- Straightforward serialization and auditing

### 5.2. Free Monads (Flows)

The flow dialect uses the free monad structure:

```math
\begin{align}
\text{Program}(A) &\cong T^F(A) \\
T^F(A) &= \mu X. \, A + F(X)
\end{align}
```

This enables:

- Binding results from prior steps
- Conditional execution paths
- Maintaining sequential dependencies

## 6. Categorical Perspective

From a categorical perspective, Effectus uses:

```math
\begin{align}
\text{List} &: \text{Free Monoid} \\
\text{Program} &: \text{Free Monad}
\end{align}
```

These free structures describe composition. They do not establish a Chomsky-hierarchy classification for the implementation.

A formal computational-power result needs a defined first-order IR and a machine-checked correspondence proof.

## 7. Comparison to Alternatives

### 7.1. General-Purpose Languages

General-purpose languages (Java, Go, Python) offer:

- Full Turing completeness
- Arbitrary recursion and side effects
- Undecidable static analysis

The checked source path instead provides:

- A finite source plan
- Static checks for known schemas and verbs
- Explicit external effect boundaries

### 7.2. Domain-Specific Rule Engines

Compared to other rule engines:

- Prolog/logic programming: Can diverge with recursive predicates
- RETE algorithms: Limited to pattern-matching, no sequential composition
- BPEL/workflow: Often Turing complete, harder to reason about

Effectus occupies a unique position with just enough power for business rules while maintaining strong guarantees.

## 8. Relationship to Total Functional Programming

Effectus shares principles with total functional programming:

```math
\begin{align}
\text{Total Function} &: \forall x \in \text{Domain}. \, f(x) \text{ terminates and produces a value in Range}
\end{align}
```

Total languages like Idris, Agda, and Coq require termination proofs. Effectus achieves similar guarantees by construction through its limited computational model rather than through proof obligations.

## 9. Conclusion: Why This Matters

The deliberate limitations in Effectus's computational model are not weaknesses but strengths:

1. **Safety**: Rules cannot crash, hang, or consume unbounded resources
2. **Verification**: Critical properties are statically verifiable
3. **Performance**: Execution has predictable resource usage
4. **Evolution**: Rule behavior remains tractable as systems grow

In practice, these benefits dramatically outweigh the theoretical limitation of not being Turing complete. For business rules, having predictable, verifiable behavior is far more valuable than unlimited computational power.
