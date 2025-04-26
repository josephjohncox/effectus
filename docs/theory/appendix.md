# Appendices — Mathematical Details and Formal Proofs

This document contains the formal mathematical foundations referenced in the main guide.

## Appendix A — Categorical Notes & Proof Sketches  

This appendix provides the theoretical foundations for our system in category theory terms.

### A.1 Free Constructions we use  

The table below shows the key mathematical structures underlying our system:

| Concept | Category $\mathcal{C}$ | Endofunctor $F$ | Free‐object |
|---------|--------------|-----------------|-------------|
| **Free Monoid** on a set $E$ | **Set** | $F(X) \coloneqq 1 \sqcup E \times X$ | $\mu X. \, 1 \sqcup E \times X$ collapses to $\text{List}(E)$ |
| **Free Monad** on the same generator | **Set** | identical $F$ | $T^E(A) \coloneqq \mu X. \, A \sqcup E \times X$ (a.k.a. *free monad*) |

What this means in practice:

* The list dialect's internal representation `[Effect]` is precisely the carrier of the free monoid on **Effect**.  
* The flow dialect's internal representation `Program A` is the free monad on the same base functor.

This mathematical correspondence explains why list rules are simpler but less powerful than flow rules.

### A.2 Canonical Embedding  

This section proves that list rules can always be converted to flow rules.

Let:  

* $\alpha : \text{List}(E) \rightarrow T^E(1)$ be the natural transformation  
  $\alpha([e_1, \ldots, e_n]) \coloneqq e_1 \star \ldots \star e_n \star \text{ret}(\langle\rangle)$,  
  where $\star$ is monadic sequencing ($\gg=$).

In plain language, $\alpha$ converts a list of effects into a sequential program by chaining the effects one after another.

#### Lemma 1  

$\alpha$ is a monoid homomorphism **and** left adjoint to the forgetful functor  
$U : T^E(1) \rightarrow \text{List}(E)$ defined by discarding bind structure.

*Proof sketch.*  
$\alpha([]) = \text{ret}(\langle\rangle)$ (preserves identity).  
$\alpha(xs \cdot ys) = \alpha(xs) \gg \alpha(ys)$ by definition of $\star$.  
Adjunction follows from the initiality of free monads: for any monoid morphism $f : \text{List}(E) \rightarrow M$, there exists a unique monad morphism $\hat{f} : T^E(1) \rightarrow M$ such that $f = \hat{f} \circ \alpha$. $\square$

**Corollary.**  
Every list-style rule set has *exactly one* representative in the flow calculus, obtained by applying $\alpha$.

This mathematical result confirms what we stated in the main document: any list rule can be uniquely converted to a flow rule, but not vice versa.

### A.3 Monotonicity under Fact-set Extension  

This section proves that adding new fields to Facts doesn't break existing rules.

Let $\Sigma_n \subseteq \Sigma_{n+k}$ be the poset of record types ordered by field inclusion.  
Define a functor:  

```math
\begin{align}
J &: \text{Struct}(\Sigma_n) \rightarrow \text{Struct}(\Sigma_{n+k}) \\
J(\text{facts}_n) &= \text{facts}_n \text{ with extra fields set to } \bot
\end{align}
```

Both `SpecList` and `SpecFlow` are morphisms in the **Set** category.  
Since both internal representations ignore fields they don't reference, we have:  

```math
\llbracket\rho\rrbracket \circ J = \llbracket\rho\rrbracket
```

for any rule file $\rho$ typed over $\Sigma_n$. $\square$

In practical terms, this means you can add new fields to your data structures without breaking existing rules - they'll simply ignore the fields they don't know about.

### A.4 Closed-world verbs = Initial Algebra Discipline  

This section explains why adding new verbs requires a code change.

$\text{Effect}$ is a sum type $\sum_i C_i$, where each constructor $C_i \coloneqq \text{Verb}_i \times \text{Payload}_i$.  
Interpreters are *algebra morphisms*  
$\phi : F(X) \rightarrow X$, where $F \coloneqq \text{Effect} \times \_$.  

Adding a constructor enlarges $F$; therefore every interpreter must be recompiled.  
This is the desired *initial-algebra* style safety valve: you cannot forget to handle a new verb.

This mathematical property ensures that when you add a new action type to your system, you must explicitly handle it in the interpreter - preventing silent failures for unhandled cases.

## Appendix B — Small-Step Semantics  

This appendix provides a formal description of how our system executes rules step by step.

### B.1 Core Syntax  

The basic building blocks of our language:

```math
\begin{align}
v &::= \langle\rangle \mid \text{effect} & \text{-- values} \\
e &::= \text{ret}(v) & \text{-- return} \\
  &\mid \text{do}(\text{effect}); e & \text{-- List dialect} \\
  &\mid \text{pure}(v) & \text{-- Flow dialect (ret)} \\
  &\mid \text{bind}(e, \lambda x.e) & \text{-- Flow dialect (bind)} \\
\text{facts} &::= \text{record of field} \mapsto \text{lit}
\end{align}
```

Note: We omit `when` blocks from this formalization. These compile to `if` guards that either produce an expression `e` or `ret ⟨⟩`.

### B.2 Evaluation Contexts  

Evaluation contexts define where computation happens next:

```math
\begin{align}
E_{\text{list}} &::= [ ] ; e \mid \text{do}(v) ; E_{\text{list}} \\
E_{\text{flow}} &::= \text{bind}([ ], \lambda x.e) \mid \text{bind}(\text{pure}(v), \lambda x.E_{\text{flow}})
\end{align}
```

These describe the order of evaluation in each dialect - the brackets `[ ]` represent where the next computation step occurs.

### B.3 Transition Rules  

These rules define how programs execute step by step. Note that $\sigma$ represents the mutable world state carried outside the calculus, treated here as a label.

#### List dialect  

```math
\frac{}{
\text{do}(v); e, \sigma \mapsto e, \sigma'
} \quad \text{if } \text{interp}(v, \sigma) \Downarrow (\langle\rangle, \sigma')
\quad \text{(Do)}
```

This rule says: when we interpret an effect, we get a new state, then continue with the rest of the program.

#### Flow dialect  

```math
\frac{}{
\text{bind}(\text{pure}(v_1), \lambda x.e_2), \sigma \mapsto e_2[x \mapsto v_1], \sigma
} \quad \text{(Bind)}
```

```math
\frac{
\text{interp}(v, \sigma) \Downarrow (u, \sigma')
}{
\text{bind}(\text{do}(v), \lambda x.e), \sigma \mapsto \text{bind}(\text{pure}(u), \lambda x.e), \sigma'
} \quad \text{(DoM)}
```

These rules show how bind operations work: (Bind) substitutes a value into a continuation, while (DoM) executes an effect before binding its result.

### B.4 Typing Rules  

```math
\frac{
\Gamma \vdash \text{effect} : \text{Verb}_i \times \tau_i
}{
\Gamma \vdash \text{do}(\text{effect}); e : \mathbf{1}
} \quad \text{(T-Effect)}
```

```math
\frac{
\Gamma \vdash e_1 : M(\alpha) \quad \Gamma, x:\alpha \vdash e_2 : M(\beta)
}{
\Gamma \vdash \text{bind}(e_1, \lambda x.e_2) : M(\beta)
} \quad \text{(T-Bind)}
```

where $M(\alpha)$ is $\mathbf{1}$ for the list dialect and $\text{Program}(\alpha)$ for the flow dialect.

These typing rules ensure that effects and binds are well-typed, preventing type errors at runtime.

### B.5 Safety Theorem  

The following theorem guarantees our system won't encounter unexpected errors during execution.

**Preservation.**  
If $\Gamma \vdash e : T$ and $e, \sigma \mapsto e', \sigma'$ then $\Gamma \vdash e' : T$.

*Proof.* By induction over transition derivations. The totality of `interp` ensures that $\text{interp}(v, \sigma)$ returns a value of the declared codomain. $\square$

**Progress.**  
For closed, well-typed $(e, \sigma)$ either:  
* $e$ is a value ($\text{ret}(\langle\rangle)$ or $\text{pure}(v)$), or  
* there exists $(e', \sigma')$ such that $(e, \sigma) \mapsto (e', \sigma')$.

*Proof.* By the canonical forms lemma and totality of `interp`. $\square$

Consequently, the system cannot get stuck except at an unmatched verb, which is precluded by the closed-world guarantee.

In practical terms, this safety theorem guarantees that well-typed rules will always either complete successfully or make progress - they won't crash or hang unexpectedly during execution (assuming proper implementation of the interpreter).
