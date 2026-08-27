# Checked execution IR

The `ir` package defines the production execution representation for Effectus.
The representation uses `effectus/v1/ir.proto` and contains no Go callbacks.

`ir.Check` validates a protobuf artifact against an immutable declaration environment.
`ir.Parse` treats stored or received protobuf bytes as untrusted input.
Both functions return an opaque `ir.Checked` value.

The checker validates these properties:

- Plan order follows list priority order, then flow priority order.
- Plan IDs and step IDs are unique.
- Step ordinals start at zero and have no gaps.
- Result slots start at zero and have no gaps.
- A result reference only uses a slot from an earlier step.
- Argument names are unique and use lexical order.
- Required and optional arguments follow the verb contract.
- Fact paths, verb contracts, functions, and types match the environment.
- Predicate functions are declared pure and total.
- Predicate results have the `bool` type.
- Literal, fact, and result values keep distinct protobuf variants.
- Structural and value limits apply before execution.
- Unknown protobuf fields cause rejection.

Use `EnvironmentDigest` for `RuleArtifact.environment_digest`.
Use `ContractHash` for each `Step.contract_hash`.
The checker recalculates both values and rejects a mismatch.

Use `Checked.Marshal` to store deterministic protobuf bytes.
Use `Checked.Digest` as the artifact content digest.
`Checked.CloneArtifact` returns an unchecked copy for inspection.
Pass a changed copy to `Check` before use.

Legacy `flow.Program` values and Go continuations are not valid checked IR.
Do not put them in a production generation.
