# Capability-Based Effects in Effectus

## 1. Introduction to Capabilities

Effectus implements a capability-based effect system that governs what actions rules can perform. This capability model provides a principled approach to resource access control and concurrency management.

```math
\begin{align}
\text{Capability} &: \text{Enum}\{Read, Modify, Create, Delete\} \\
\text{Verb} &: \text{Name} \times \text{Capability} \times \text{ArgTypes} \times \text{ReturnType} \\
\text{Effect} &: \text{Verb} \times \text{Args} \times \text{Result}
\end{align}
```

Every verb in the system is associated with exactly one capability, establishing what level of access it requires to execute. This creates a static permission model that can be reasoned about at compile time.

## 2. Theoretical Foundation

### 2.1. Capability Lattice

The capabilities form a lattice with a natural ordering:

```math
Read \leq Modify \leq Create \leq Delete
```

This ordering reflects increasing levels of authority, where each capability implies all capabilities below it in the lattice. The lattice structure enables formal reasoning about permission containment.

### 2.2. Capability as a Type Refinement

Conceptually, capabilities refine the Effect type:

```math
\begin{align}
\text{Effect}_C &: \{ e : \text{Effect} \mid \text{cap}(e) \leq C \}
\end{align}
```

This refinement type restricts effects to those with capabilities not exceeding a given bound $C$. This enables static verification that a rule doesn't perform operations beyond its authority.

## 3. Operational Semantics with Capabilities

Capabilities extend the operational semantics of Effectus by introducing resource locking:

```math
\begin{align}
\text{acquireLock}(e) &: \text{Capability} \times \text{Key} \rightarrow \text{Lock} \\
\text{releaseLock}(l) &: \text{Lock} \rightarrow \text{Unit}
\end{align}
```

The execution of effects is governed by this lock discipline:

```math
\begin{align}
\frac{\text{cap}(e) = c \quad \text{key}(e) = k \quad l = \text{acquireLock}(c, k)}
     {\langle e, \text{ctx} \rangle \Rightarrow \langle \text{result}, \text{ctx'} \rangle} \quad \text{releaseLock}(l)
\end{align}
```

In practical terms, this means that before executing an effect, we must acquire the appropriate capability lock on the target resource. After execution, we release the lock.

## 4. Key Properties

### 4.1. Capability Safety

A key property of the capability system is **capability safety**:

**Theorem (Capability Safety)**: A rule with maximum capability $C$ cannot perform any effect $e$ where $\text{cap}(e) > C$.

This is enforced statically during rule compilation, preventing unauthorized operations.

### 4.2. Deadlock Freedom

The capability order provides a natural locking order, which ensures deadlock freedom:

**Theorem (Deadlock Freedom)**: If locks are always acquired in order of increasing capability, then no deadlock can occur.

In the implementation, the lock manager enforces a total ordering of lock acquisition based on capability and resource key, preventing circular wait conditions.

## 5. Practical Applications

The capability system enables several important features:

1. **Resource Protection**: Critical resources are protected by appropriate capability requirements
2. **Concurrency Control**: The locking discipline ensures safe concurrent execution of rules
3. **Authorization Modeling**: Business permissions map naturally to capability requirements
4. **Static Verification**: Rule capabilities can be statically verified before deployment

## 6. Relationship to Other Typed Effect Systems

Effectus's capability model relates to other typed effect systems:

| System | Approach | Notable Difference |
|--------|----------|-------------------|
| **Haskell IO Monad** | Type-based isolation | No fine-grained permission model |
| **Rust Borrowing** | Ownership & lifetime | Focuses on memory safety |
| **Object Capability** | Reference possession | Dynamically checked at runtime |
| **Effectus** | Capability lattice | Statically verified, resource-oriented |

The Effectus approach combines static verification with resource-oriented capabilities, providing safety guarantees while maintaining runtime efficiency.

## 7. Future Extensions

Possible extensions to the capability system include:

1. **Delegated Capabilities**: Temporarily granting capabilities to sub-operations
2. **Parametric Capabilities**: Capabilities that depend on rule parameters
3. **Derived Capabilities**: Defining new capabilities as combinations of existing ones

These extensions would further enhance the expressiveness while maintaining the core safety properties of the system. 