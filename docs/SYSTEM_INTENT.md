# System Intent

Effectus turns typed facts into checked plans and durable dispatch records.
The current execution contract is defined by [Runtime Guarantees](GUARANTEES.md) and [Runtime Lifecycle](LIFECYCLE.md).

## Core intent

- **Deterministic planning:** fixed facts and a fixed checked generation determine plan selection and step order. External operations need not be deterministic.
- **Checked contracts:** startup compilation and validation reject unsupported expressions, unknown declarations, and incompatible argument bindings before admission.
- **Immutable executable identity:** one startup generation serves new admissions. A rule or descriptor change requires a new source bundle and process replacement.
- **Pinned recovery:** unfinished executions keep their recorded artifact identity. Replacement processes resolve historical artifacts rather than reinterpret work against new rules.
- **Durable intent:** the runtime records admission and dispatch intent before invoking an external operation.
- **Explicit outcomes:** admission, completion, failure, and blocked states remain distinct. An unknown external outcome does not prove that retry or compensation is safe.

There is no hot-reload, candidate-activation, or automatic deployment-rollback API in the daemon.
Operators can deploy a previously retained bundle through process replacement. They must still preserve artifacts required by existing executions.

## Contract boundaries

Facts and verb contracts define supported value types.
Capability and resource declarations describe access requirements. They do not grant destination permissions or prove that operations commute.
Inverse declarations describe compensation. Compensation is another external operation, not an atomic reversal of distributed state.

A destination must enforce business idempotency and any required fencing.
Effectus cannot guarantee exactly-once destination effects through transport metadata alone.

## Resource lifetime

Shutdown stops admission and cancels intake workers.
The daemon drains and joins handlers and workers before closing the engine, then closes the database.
Context cancellation is cooperative. A callback that ignores cancellation can prolong shutdown beyond a drain deadline.
