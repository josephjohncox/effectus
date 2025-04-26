# Effectus Rules — A Concise Type-Theoretic Guide  

## 1. Surface Language, Core Language

This section describes the syntax of our rule languages and how they map to internal representations.

At its core, Effectus provides two distinct ways to express business rules:

1. **List-style rules** (.eff files): These express simple "when this, then that" relationships where all effects happen in parallel.
2. **Flow-style rules** (.effx files): These allow for more complex, sequential operations where each step can depend on the results of previous steps.

```
rule      ::=  when { p₁ … pₙ } then { e₁ … eₖ }          --  .eff   (list)
flow      ::=  when { p₁ … pₙ } steps { s₁ … sₘ }         --  .effx  (free-monad)
step      ::=  verb arg* [ "->" x ]                       --  binds result
predicate ::=  path op lit | path op list | …
```

After parsing, we elaborate every file into one of two internal calculi:

| Dialect | Core term | What it means |
|---------|-----------|---------------|
| **list** | `SpecList  = Facts → [Effect]` | A pure function that returns an ordered sequence of effects |
| **flow** | `SpecFlow  = Facts → Program Unit` | A pure function that returns a program with sequential operations |

`Program A` is the **free monad** over the base functor $F(X,A) = \text{Effect} \times (A \rightarrow X)$.

In simpler terms, the list dialect produces a simple batch of actions to perform, while the flow dialect produces a recipe that can adapt as it executes, with each step potentially influencing what happens next.

## 2. Types

Here we define the core types in our system:

```math
\begin{align}
\text{Facts}   &: \Sigma\text{-type} & \text{-- extensible record from protobuf descriptors} \\
\text{Verb}    &: \text{closed sum type} & \text{-- e.g., Reject | Warn | ReserveMaterial | …} \\
\text{Effect}  &: \text{Verb} \times \text{Payload} & \text{-- payload: any well-typed value} \\
\text{Program} &: \mu X. \, A + (\text{Effect} \times (R \rightarrow X)) & \text{-- fixpoint (free monad)}
\end{align}
```

What these types represent in practice:

- **Facts**: The input data your rules operate on. Think of this as a structured document with all the information needed to make decisions. It's extensible, meaning new fields can be added over time without breaking existing rules.
- **Verb**: The specific actions your system can take - rejecting an order, issuing a warning, reserving materials, etc. Unlike Facts, this set is closed and carefully controlled.
- **Effect**: A combination of a verb with its specific payload. For example, RejectOrder("insufficient funds") pairs the rejection action with the reason.
- **Program**: A sequence of effects that can branch or adapt based on results of previous steps. The mathematical definition uses a fixpoint to allow for arbitrary chains of operations.

There are two key properties about our type system:

* Facts are **open‐world**: You can add new $\Sigma$-components in later proto packages without breaking compatibility.  
* Verbs are **closed‐world**: Adding a new verb requires changing the code and adding a new interpreter case.

This distinction is crucial for system evolution: you can freely extend what information your rules can access, but carefully control what actions they can perform.

## 3. Denotational Semantics

We interpret our terms in the mathematical **Set** category, giving them precise meaning:

| Term | Meaning in Set theory |
|------|------------|
| `Facts` | Product set $\prod_{i} \llbracket\tau_i\rrbracket$ |
| `Effect` | Coproduct $\sum_{i}(\text{Verb}_i \times \llbracket\text{Payload}_i\rrbracket)$ |
| `Program A` | Free monad $T^E(A) \cong \mu X. \, A + (\text{Effect} \times X)$ |
| `SpecList` | Function $\llbracket\text{Facts}\rrbracket \rightarrow \text{List}(\text{Effect})$ |
| `SpecFlow` | Function $\llbracket\text{Facts}\rrbracket \rightarrow T^E(\text{Unit})$ |

In practical terms:
- Facts are like a database record with multiple fields
- An Effect is one of several possible actions with its associated data
- A Program is a sequence of effects that can adapt based on intermediate results
- A SpecList is a rule that maps input data to a list of actions
- A SpecFlow is a rule that maps input data to a complex process with multiple steps

Two important observations:  

* `List(Effect)` is the **free monoid** on `Effect` - essentially a sequence with no additional structure.  
* `Program` is the **free monad** on the endofunctor $F(X) = \text{Effect} \times X$. This means `List` can be embedded into `Program` via the canonical monoid‐to‐monad conversion $\text{Sequence} : \text{List}(\text{Effect}) \rightarrow \text{Program}(\text{Unit})$.  
  This `Sequence` function is the left adjoint to the forgetful functor from monads to monoids.

What this means in practice: any rule written in the simpler list style can be automatically converted to the more powerful flow style, but not vice versa. This is like how any simple sequential script can be rewritten as a complex program with branches and conditions, but the reverse isn't always possible.

## 4. Operational Semantics (Small-Step)  

This section explains how rules are executed step by step. The mathematical formulation ensures deterministic and predictable behavior.

### 4.1 Evaluation of Predicates  

```math
\frac{\langle \text{facts}, \text{path} \rangle \Downarrow \text{value}}
     {\langle \text{facts}, \text{path} \mathbin{\text{op}} \text{lit} \rangle \Downarrow \text{bool}}
     \quad \text{(cmp)}
```

This rule describes how we evaluate conditions in our rules. In plain English:
1. First, we look up a value at a certain path in our facts
2. Then, we compare that value with a constant using some operator (like equals, greater than, etc.)
3. The result is a boolean - true or false

Path lookup always works thanks to schema validation at compile time. When a module is missing, we get a special $\bot$ value, and any comparison with $\bot$ evaluates to `false`. This fail-safe behavior ensures rules won't crash on missing data.

### 4.2 List Dialect  

```math
\begin{align}
\text{when}(f, ps) &= \bigwedge_{p \in ps} \text{eval}(p, f) \\[0.5em]
\text{execList}(\emptyset, \text{ctx}) &\Rightarrow \text{ctx} \\
\text{execList}(e::es, \text{ctx}) &\Rightarrow \text{execList}(es, \text{interp}(e, \text{ctx}))
\end{align}
```

This defines how list-style rules execute:
1. A rule triggers when all its conditions evaluate to true (the `when` function)
2. For an empty list of effects, we just return the current context (we're done)
3. For a non-empty list, we interpret the first effect, update the context, and then continue with the rest

The `interp` function is our domain-specific **executor**. While it has side effects, we consider it semantically *pure* when viewed through the lens of the State or IO monad. This means we can reason about it mathematically while it still performs real-world actions like updating databases or sending notifications.

### 4.3 Flow Dialect  

The inductive rules for `Program` follow Plotkin-style operational semantics:

```math
\begin{align}
&\text{β-step}: \\
&\frac{}{\langle \text{Do}(e) \gg= k, \text{ctx} \rangle \mapsto \langle k(r), \text{ctx'} \rangle} \quad \text{where } (r, \text{ctx'}) = \text{interp}(e, \text{ctx}) \\[1em]
&\text{ret}: \\
&\frac{}{\langle \text{Pure}(a), \text{ctx} \rangle \mapsto \langle a, \text{ctx} \rangle}
\end{align}
```

These rules define how flow-style programs execute:
1. The β-step rule handles the core operation: perform an effect, get a result and updated context, then continue with the next operation that can depend on the result
2. The ret rule handles the end of a computation, just returning a value

The system is deterministic because both `interp` (given a fixed executor) and rule evaluation are pure functions. This ensures that running the same rule on the same data always produces the same result - a critical property for business systems.

## 5. Type Soundness Sketch  

Our type system prevents runtime errors through two key properties:

* **Progress**: A well-typed `Facts` value plus a compiled `SpecList` (or `SpecFlow`) always produces either:  
  * A completed value ($\emptyset$ or $\text{Pure}(\text{unit})$), or  
  * A configuration that can take another step ($e::es$ or $\text{Do}(e) \gg= k$).  

* **Preservation**: Each reduction step maintains well-typedness; `interp` is defined for all values in the closed set of `Verb`.

In simpler terms, our system guarantees that:
1. Rules will never get "stuck" in the middle of execution
2. Types remain consistent throughout execution
3. Every possible action is handled by the interpreter

This means evaluation can never "get stuck" except for a missing interpreter case—which we prevent by keeping verbs in a closed world. This guarantee is crucial for production systems where unexpected failures are unacceptable.

## 6. Extensibility Lemma  

This explains how our system handles evolution over time:

Let $\Sigma_n$ be the set of fact fields in version $n$.  
For any rule file $\rho$ compiled against $\Sigma_n$, and any extended set $\Sigma_n \subset \Sigma_{n+k}$,  
$\llbracket\rho\rrbracket$ behaves identically when run on the extended data: the system simply ignores extra fields during path lookup.  
This makes the system **monotone with respect to fact growth** - adding fields doesn't break existing rules.

In practical terms:
- You can add new data fields to your system without breaking existing rules
- Older rules simply ignore the new fields they don't know about
- This allows for graceful system evolution without requiring updates to all existing rules

On the other hand, adding a new verb extends what `interp` needs to handle. Older binaries can't interpret new verbs, which intentionally forces a redeploy. This is a safety mechanism: new actions require explicit handling in the system.

## 7. Mix-in Composition Properties  

The *Include* mechanism works at the text level, making rule elaboration both associative and idempotent. In plain terms, `include A; include A` duplicates rules but doesn't change the meaning unless rule names collide.

Before elaboration, we establish a **total order** on rules, ensuring that mix-ins combine predictably with compilation.

This property enables a modular approach to rule development:
- Teams can develop rule sets independently
- Common rules can be shared via includes
- The system guarantees consistent behavior regardless of how rules are combined
- Rule ordering is explicit and predictable, avoiding subtle interaction bugs

## 8. Practical Take-aways  

Why this matters for actual systems:

* **List rules** are *monoids*—simpler to understand, can run in parallel, and handle 95% of QA/compliance logic.  
* **Flow rules** are *free monads*—more powerful, supporting sequential operations when you need them.  
* Shared grammar and schema validation catch errors early, before runtime.  

The mathematical foundations directly translate to real-world benefits:
- Simple rules for simple cases (list dialect)
- Powerful rules when you need them (flow dialect)
- Strong guarantees about system behavior
- Early error detection through static analysis
- Safe extensibility as requirements evolve
- Predictable composition of independently developed rules

This combination of simplicity, power, and safety makes Effectus ideal for critical business logic that needs to be both reliable and adaptable over time.

*(For deeper mathematical details, see appendix A for categorical proofs and appendix B for complete small-step semantics.)*
