# M5 supported API validation

## Status

**R01–R27 are implemented, validated, and independently accepted.**
Combined review `a1eb959e-6999-425d-8f10-e9f7109c3374` accepted M5 with a formatting-snapshot note, reconciled below.
R28–R40 remain open. This acceptance does not claim production readiness.

## R25: Implemented and independently accepted

[The capability matrix](../grpc-capabilities.md) lists all three services and all 19 RPCs.
Only `RulesetExecutionService.ExecuteRuleset` is implemented by the shipped server.

Reserved RPCs now carry service or method deprecation markers.
The deprecated request aliases and unpopulated response fields have explicit comments.
The contract distinguishes the legacy `CompiledRuleset` payload from checked IR artifacts.
No service name, method name, message name, or field number changed.

`runtime/grpc_capabilities_test.go` calls all 18 reserved RPCs against a real server, including the stream.
It checks `Unimplemented` responses and descriptor deprecation markers.
It detects service or method additions that require a capability review.

Validation passed:

- Full repository race tests, including the M4 storage-classification correction.
- Protobuf lint and compatibility against HEAD.
- Repository guardrails, without inventory or budget changes for R25.
- Byte-identical Go generation across a second generation pass.
- Primary LSP checks on the new test and generated bindings.

Results: `out/remediation/m5-r25-results.json` and the corresponding logs.
The combined M5 review independently accepted the capability contract and its source-backed tests.

## R26: Implemented and independently accepted

The first boundary regressions reproduced constructor filesystem mutation, nil-input panics, and canceled registrations that changed input values.
The red log is `out/remediation/m5-r26-boundary-red.log`.
The initial correction was partial when commit `673d6c9` was published.

The completed correction:

- Keeps constructor reads free of filesystem writes and rejects invalid receivers, contexts, schemas, names, and scalar types.
- Owns registered values and returns deep snapshots, including nested metadata and numeric types.
- Serializes registration, generation, validation, and count reads within one integration.
- Honors one configured v2 module path and configured v1/v2 generation output paths.
- Propagates command failures and cancellation. Failed validation no longer reports success.
- Assigns new field numbers deterministically and never overwrites different existing protobuf definitions.
- Uses confined filesystem operations and atomic no-overwrite installation.
- Documents the trusted-workspace, single-owner, compatibility-only contract in [the Buf guide](../buf-compatibility.md).

The wrapper remains compatibility support without a new removal commitment.
An initial formal Go deprecation marker failed the removal-deadline guard.
That newly added marker was replaced with usage guidance. No guard or inventory budget changed.

Validation passed:

- Schema race tests with `-count=3`: `m5-r26-hardening-race.log`.
- Full repository race tests: `m5-r26-full-race.log`.
- `go vet ./schema`: `m5-r26-vet.log`.
- Final repository guardrails: `m5-r26-guardrails-final.log`.
- Primary LSP checks for the changed Go files.

These logs are under `out/remediation/`.
`m5-r26-final-results.json` records the full suite and the initial guard failure.
`m5-r26-guardrails-final-result.json` records the corrected guard success.

Independent review `d38abdd6-122c-4ba0-a211-dcbd78a7bbe6` returned **ACCEPT for R26 only**.
The reviewer read source, regression tests, and parent-provided command evidence. It did not rerun commands.
The exact reviewed source hashes and diff are in `m5-r26-review-manifest.json` and `m5-r26-review.patch`.
That first review accepted R26 only. The later combined review accepted all of M5, not production readiness.

## R27 and review

[The Go API guide](../go-api.md) now maps all 16 inventoried package paths.
It separates recommended entry points, low-level infrastructure, generated declarations, and compatibility exports.
It covers ownership, concurrency, contexts, shutdown, typed failures, accepted-only execution, and historical replay.

The inventory audit counted 1,768 declarations, including 1,248 generated declarations.
These are declaration counts, not feature counts. No public declaration or budget changed.
Package comments now explain runtime and schema ownership boundaries.
Embedded entry points and engine construction have explicit lifetime and default-storage comments.

The complete repository race suite and guardrails passed after the R27 documentation changes.
Primary LSP checks passed. STE lint completed with advisory style counts, not a correctness verdict.
Results: `out/remediation/m5-api-results.json` and its logs.
All 12 files in the accepted R26 manifest remained byte-identical.

The combined M5 review independently accepted R27 with R25 and the previously accepted R26.
Its report is preserved at `out/remediation/m5-api-accepted.md`.
The reviewer checked source contracts and parent-provided test evidence. It did not rerun commands.

## Exact acceptance snapshot

The original manifest is `out/remediation/m5-api-review-manifest.json`.
During review, readbacks showed four added blank lines in `embedded/embedded.go`.
The reviewer found no semantic difference and requested final hash reconciliation.

The parent removed only those four blank lines in memory and reproduced the original frozen SHA256 exactly.
The file grew from 3,644 to 3,648 bytes. All other 29 frozen file hashes stayed unchanged.
The reconciled snapshot is `out/remediation/m5-api-review-manifest-format-update.json`.
The exact diff and verification are in `m5-api-format-only.patch` and `m5-api-format-drift-evidence.json` under `out/remediation/`.
No source edit or validation weakening was needed to reconcile the snapshot.
A live guidance attempt arrived after the reviewer finished. Its final verdict already included the same snapshot note.

These manifests preserve the accepted application and guide snapshots.
Later edits to this validation report and the checklist record acceptance. They are not application changes.

## Tool evidence

A bounded read-only lookup helper stopped after repetitive tool calls. It made no source changes.
The parent completed the registration check through a bounded search with an explicit repository path.
Only `runtime/execution_grpc.go` registers a generated service outside generated code.

A local dependency-document read hit the context tool's workspace boundary.
That boundary was not bypassed. Version-pinned public protobuf documentation supplied the required reflection API signatures instead.
