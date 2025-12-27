# Glossary

- **Fact**: A typed piece of data used by rules (e.g., `customer.email`). Facts can be merged from multiple sources.
- **Fact Schema**: The authoritative shape and type for a fact path (Proto or JSON Schema). May be versioned.
- **Fact Source**: External system that produces facts (Kafka, CDC, SQL, S3, gRPC, etc.).
- **Verb**: A named effect/operation with typed inputs and a typed return value.
- **Inverse Verb**: The compensating verb that reverses a mutating verb.
- **Capability**: The declared permission and concurrency semantics a verb requires (read/write/create/delete + idempotent/commutative/exclusive).
- **Rule**: A list-style `when/then` block that emits effects when predicates are true.
- **Flow**: A step-based sequence of verbs with optional branching and bindings.
- **Expression**: A boolean predicate evaluated against facts using the standard function library.
- **Ruleset**: A compiled collection of rules and flows with their schemas and verb specs.
- **Bundle**: A versioned, distributable package of schemas, verbs, and rules.
- **Extension**: A mechanism for loading verbs, schemas, and functions (static, JSON, or bundles).
