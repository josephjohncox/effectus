# Simplified Appendices — The Math Made Accessible

This document provides a more approachable explanation of the mathematical foundations behind Effectus.

## Appendix A — The Math Behind List vs. Flow

### A.1 Two "Free" Constructions on Effects

We start with a set of **Effects** (our verbs + payloads), call it **E**. From that one generator **E** we get two important structures:

1. **Free Monoid** on E:  
   - Mathematically defined as:  
    \[
       \text{List}(E) \cong \mu X. \, 1 + E \times X
    \]  
   - In plain language: a finite list \([e_1, e_2, \dots, e_n]\) of effects.  
   - Basic laws:  
     - **Identity**: Empty list \([]\) acts as a neutral element
     - **Associativity**: \((xs \mathbin{++} ys) \mathbin{++} zs = xs \mathbin{++} (ys \mathbin{++} zs)\)

2. **Free Monad** on E:  
   - Mathematically defined as:  
     \[
       T^E(A) \cong \mu X. \, A + E \times X
     \]  
   - In plain language: a mini-program that can perform an effect and then continue based on results.  
     \[
       \text{Pure}(a) \quad \text{or} \quad \text{Do}(e, \, k: R \to T^E(R))
     \]  
   - Basic laws:  
     - **Left unit**: \(\text{Pure}(a) \gg= f = f(a)\)  
     - **Right unit**: \(m \gg= \text{Pure} = m\)  
     - **Associativity**: \((m \gg= f) \gg= g = m \gg= (x \mapsto f(x) \gg= g)\)

> **Key Takeaway**  
> - **List** is "just collect effects" - simple but limited.  
> - **Program/Monad** is "collect effects plus thread results forward" - more powerful but complex.  

### A.2 Embedding the List into the Monad

There is a **canonical way** to convert any list into a program:  
\[
  \alpha : \text{List}(E) \longrightarrow T^E(1)
\]  

Defined by:  
\[
  \begin{align}
    \alpha([]) &= \text{Pure}(\langle\rangle) \\
    \alpha([e_1,\ldots,e_n]) &= \text{Do}(e_1, \lambda\_. \, \text{Do}(e_2, \lambda\_. \, \ldots \, \text{Do}(e_n, \lambda\_. \, \text{Pure}(\langle\rangle))\ldots))
  \end{align}
\]  

This conversion:
- **Preserves the structure** (empty list → Pure, concatenation → bind chain)
- Allows every simple rule set to be converted to the flow world without losing information

In practical terms, any list-style rule can be automatically rewritten as a flow-style rule, but the reverse isn't always possible.

### A.3 Open Facts vs. Closed Verbs

- **Facts** form a large record type \(\Sigma\) that can **grow** over time as we add new fields.  
- **Verbs** form a sum type \(E\) that is **fixed** (closed) for each version of the code.  

**Monotonicity property**: If you add new fields to \(\Sigma\), old rules still work correctly because they simply ignore the extra fields they don't reference.

This gives us a clean way to evolve our system: freely extend the data we can access, but carefully control the actions we can take.

## Appendix B — How Execution Really Works

### B.1 Predicate Checking ("when")

For each rule with conditions \(p_1, \dots, p_n\):

1. **Lookup** each path in your typed Facts record → get a value \(v\) or "missing"  
2. **Compare** according to the operator  
   \[
     v \; \texttt{op} \; \ell \; \longmapsto \; \mathit{true/false}
   \]  
3. **Guard**: the rule fires if and only if all comparisons yield `true`

This mechanism ensures rules only activate when all their conditions are met.

### B.2 List-Mode Execution

```pseudo
function runList(rules, facts, executor):
  for rule in rules:
    if allPredsTrue(rule.when, facts):
      for effect in rule.then:
        executor.Do(effect)
```

List execution is:
- **Sequential**: effects happen in order
- **Deterministic**: same inputs always produce same outputs
- **Simple**: only needs the basic monoid properties

### B.3 Flow-Mode Execution

Each flow rule compiles to a small **Program**:

```pseudo
do e1 ← Do(effect₁)
   e2 ← Do(effect₂_using e1)
   …
   return ()
```

At runtime, we "step" this program in a systematic way:

1. **Do**: call `executor.Do(effect)` → get a result  
2. **Bind**: plug that result into the next continuation  
3. **Pure**: finish with a value (typically `()`)

Because this is a free monad, all the usual monad laws apply, and you get full data-flow between steps - each step can use results from previous steps.

### B.4 Safety Guarantees

Our system provides several important guarantees:

- **Type checking** ensures every `path` exists in the Facts schema and you never compare incompatible values
- **Closed-world verbs** force you at compile time to handle or explicitly reject new verbs in your executor
- **No stuck states**: every well-typed rule either finishes successfully (List: runs out of effects; Flow: reaches Pure) or makes a valid next step

These guarantees combine to make a robust system that catches errors early and behaves predictably at runtime.

