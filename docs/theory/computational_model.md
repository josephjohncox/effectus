# Computational Bounds

This document explains the bounds of the checked first-order IR.

It does not classify the complete Effectus repository as Turing complete or non-Turing complete.

## Finite artifact

A checked artifact is a finite protobuf value. The parser and checker apply explicit limits to its size and structure.

A plan contains a finite number of steps. Result references form an acyclic order because each reference points to an earlier slot.

These properties bound the number of internal plan steps for one selected plan.

## Internal termination condition

Internal checked evaluation terminates under these assumptions:

1. Fact lookup terminates.
2. Each supported pure predicate function terminates.
3. Literal and operator evaluation terminates.
4. The evaluator does not add new plan steps.
5. Retry policy has a finite attempt bound.

Under these assumptions, selection and plan traversal have no unbounded recursion.

## What this does not bound

The finite plan does not bound:

- External verb latency
- Remote computation
- Network retries below Effectus
- Operator time to resolve a blocked outcome
- Database size across many executions
- Legacy Go continuation behavior

A caller deadline can bound waiting time. It cannot force a remote system to stop work.

## Determinism condition

Checked selection is deterministic for a fixed artifact, environment, fact payload, and pure function implementation.

Stable priority and source order make equal-priority selection repeatable.

External execution is not generally deterministic. A verb can read a clock, database, queue, or remote service.

## Resource analysis

The checked artifact makes these values directly inspectable:

- Rule count
- Plan count
- Step count
- Argument count
- Result dependencies
- Declared capabilities
- Declared executor contracts

The artifact does not reveal the full time or space cost of an external destination.

## Why first-order IR helps

A first-order artifact supports deterministic serialization, content hashing, validation, storage, comparison, and replay of recorded intent.

A Go continuation closes over process memory. It does not provide these properties without an additional serialization model.

## Relation to general computation

The source grammar and checked IR are domain-specific representations. The Chomsky hierarchy classifies languages, not runtime effect systems.

A computational-power theorem would need a precise encoding, operational semantics, and proof. These notes make no such theorem.

## Operational conclusion

Use the finite-plan property to reason about internal traversal and dependency order.

Use deadlines, bounded retries, leases, and blocked states to control external execution.

Do not infer unconditional termination or fixed resource use from a finite checked plan.
