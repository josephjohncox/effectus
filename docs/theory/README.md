# Theory Notes

These documents describe semantic models for Effectus. They are not proofs of the complete Go implementation.

Use [Runtime Guarantees](../GUARANTEES.md) for implemented behavior. Use [Executable State Models](https://github.com/josephjohncox/effectus/blob/main/formal/README.md) for the bounded TLA+ models.

## Scope

The notes separate three systems:

1. The checked first-order IR used by production effectusd
2. Legacy Go list and continuation APIs used by embedded applications
3. External verb destinations that Effectus does not control

A property must name the system and assumptions that it covers.

## Documents

- [Core Model](basic.md) defines facts, plans, steps, values, and execution states.
- [Computational Bounds](computational_model.md) explains finite checked plans and termination assumptions.
- [Compensation](compensation.md) models durable forward and reverse dispatch.
- [Capabilities](capabilities.md) explains conflict metadata, local locking, and fencing.
- [Verb Extensions](verb_extension.md) models contracts and executor interpretation.
- [Notation and Proof Obligations](appendix.md) collects the abstract transition rules.

## Terminology

A checked artifact is a finite protobuf value that passes `ir.Check`.

A plan is an ordered sequence of checked steps. A result slot can refer only to an earlier step.

An interpreter maps a checked verb invocation to an external operation and an outcome.

A generation is an immutable environment plus its checked artifacts and executors.

## Claim levels

These notes use three claim levels:

- **Implemented invariant**: repository tests or a checked model cover the stated boundary.
- **Model property**: the property follows from the abstract model and its assumptions.
- **Proof obligation**: a full implementation proof would need to establish the property.

No theory document establishes exactly-once external delivery, semantic inversion, or unconditional termination.

## Main distinctions

Checked rule evaluation can be deterministic for a fixed artifact, environment, and fact payload.

External verb execution can depend on networks, clocks, databases, and remote policy. It is not generally deterministic.

A finite checked plan bounds internal step count. It does not bound external latency, retries, storage growth, or remote resource use.

Compensation is a second external operation. It is not an ACID rollback.

## Contribution rules

- State all assumptions next to each property.
- Distinguish checked IR from legacy Go continuations.
- Distinguish internal durability from external enforcement.
- Link runtime claims to tests, models, or the normative guarantee document.
- Do not use category-theory terminology when an ordered state machine is sufficient.
