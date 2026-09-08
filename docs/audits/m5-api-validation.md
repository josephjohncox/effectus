# M5 supported API validation

## Status

R01–R24 are independently accepted. R25–R27 remain unchecked until the M5 review.
R28–R40 also remain open.

## R25: Implemented, not yet independently accepted

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
No independent R25 acceptance is claimed yet.

## R26: Partial implementation

The first boundary regressions reproduced constructor filesystem mutation, nil-input panics, and canceled registrations that changed input values.
The red log is `out/remediation/m5-r26-boundary-red.log`.

The partial correction:

- Rejects an empty workspace and resolves the root to an absolute path.
- Reads configuration without creating directories or writing a default configuration.
- Rejects nil or uninitialized integrations, nil contexts, nil schemas, and already-canceled contexts before operations.
- Rechecks cancellation after registration or generation lock acquisition.
- Rejects unsafe schema names and invalid protobuf field identifiers before filesystem writes.

Repeated schema race tests pass with `-race -count=3`.
Results: `out/remediation/m5-r26-boundary-result.json` and its race log.
Primary LSP checks passed. The session diagnostics report no blocking errors across the diagnosed files.
Auxiliary coverage remains incomplete where previously reported.

Required remaining work:

1. Own registration inputs and return deep copies from getters and lists.
2. Coordinate registration, generation, validation, and registry-count reads.
3. Honor configured generation outputs instead of the hard-coded legacy directory.
4. Propagate validation cancellation and command failure correctly.
5. Add nil getter/list behavior and concurrent-access regressions.
6. Resolve unsafe legacy generation behavior before describing the wrapper as safe.
   Map iteration currently assigns field numbers nondeterministically and overwrites existing schema files.
   Preserve existing wire identities or reject unsafe updates with actionable migration guidance.
7. Document the compatibility-only role and limits of this exported wrapper.

No complete R26 hardening or deprecation decision is claimed.
`schema/buf_integration.go` still contains the unfinished behavior listed above.

## R27 and review

The supported Go package guides, ownership contracts, and export audit remain unfinished.
Do not mark M5 accepted until these requirements and an independent review pass.

## Tool evidence

A bounded read-only lookup helper stopped after repetitive tool calls. It made no source changes.
The parent completed the registration check through a bounded search with an explicit repository path.
Only `runtime/execution_grpc.go` registers a generated service outside generated code.

A local dependency-document read hit the context tool's workspace boundary.
That boundary was not bypassed. Version-pinned public protobuf documentation supplied the required reflection API signatures instead.
