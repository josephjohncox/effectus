# System Intent

Effectus exists to make rule execution deterministic, explainable, and safe under change. The engine turns **facts** (typed, versioned data from multiple sources) into **effects** (verbs that do real work) using rules and flows that are validated before they ever run.

## Core Intent

- **Deterministic outcomes**: the same facts and rules produce the same effects. Time, randomness, and IO are explicit and traceable.
- **Typed contracts everywhere**: fact schemas and verb interfaces are the source of truth. Runtime enforcement is optional but available for strict environments.
- **Compile before run**: rules and flows are parsed, type-checked, and linted before deployment. Fail fast, not in production.
- **Explicit capabilities**: every verb declares required capabilities and resources to enable safe planning, concurrency rules, and security checks.
- **Hot reload without chaos**: new rulesets are compiled and validated before swapping into production, with safe rollback on failure.
- **Composable bundles**: rules, schemas, and verbs are packaged, versioned, and resolved with checksums and compatibility constraints.

## What “Correct” Means

- A rule cannot reference unknown facts or call verbs with incompatible types.
- A mutating verb must declare its inverse and concurrency semantics.
- Expressions cannot rely on unsafe or nondeterministic operations without an explicit policy.
- Facts can come from multiple sources, but their merge rules must be explicit and repeatable.

If a change makes any of the above ambiguous, it is a bug by definition.
