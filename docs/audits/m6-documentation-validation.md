# M6 documentation and onboarding validation

## Status

R01–R27 have independent acceptance.
R28 and R29 passed independent acceptance, including R28's corrected unknown-outcome policy distinction.
R30 and R31 have independent acceptance. See [tutorial and contributor evidence](m6-tutorial-contributor-validation.md), including the verified six-blank-line snapshot amendment.
Their tests also replace part of the lexical-only documentation checks under R34.
R32 passed independent acceptance after the parsed authentication-header whitespace correction and raw TCP regression. See [HTTP reference evidence](m6-http-reference-validation.md).
R33 passed independent acceptance for live authenticated Go/Python execution and separate TLS validation. See [client evidence](m6-grpc-client-validation.md).
R34 and R35 passed independent acceptance. The correction re-review also accepted combined M6 consistency.
See [combined M6 acceptance and snapshot evidence](m6-contract-site-validation.md#combined-m6-acceptance).
R36–R40 remain open.

## R28: Current lifecycle, not a hot-reload design

The current lifecycle corrections cover:

- `docs/ARCHITECTURE.md`
- `docs/SYSTEM_INTENT.md`
- `docs/coherent_flow.md`
- `docs/LIFECYCLE.md`
- `docs/design.md`
- `docs/GUARANTEES.md`
- `docs/EXTENSION_SYSTEM.md`
- `docs/theory/verb_extension.md`
- `docs/theory/appendix.md`

These pages now distinguish one startup admission generation from historical generations resolved for replay and recovery.
Rule or descriptor changes require a new bundle and process replacement, not runtime refresh or candidate activation.
The formal appendix retains abstract publication and pinning properties but labels them as model transitions, not daemon features.

The shutdown sequence follows `cmd/effectusd/services.go`:
stop HTTP admission, cancel intake, start gRPC shutdown, drain HTTP, join handlers and workers, then close the engine and database.
Deadline expiry triggers cancellation, not forced Go callback termination.
Pending durable work can remain for recovery. Shutdown does not promise completion of every accepted identity.

The correction also removes stale embedded-continuation claims and separates planning determinism from external-operation outcomes.
Fencing no longer appears as a substitute for destination deduplication.
The destination must coordinate deduplication with its business commit and enforce fencing when stale ownership threatens safety.

Source checks included `openDaemon`, `httpHandler`, and `daemonServices.run`.
The HTTP route still uses accepted-only execution. An explicit generation constraint applies to pinned replay or active new admission.

## R28 validation

Actual commands and results are in `out/remediation/m6-r28-results.json`.

- `go test -race -count=3 -timeout=90s ./runtime ./cmd/effectusd -run 'Shutdown|Recovery|Generation|Close'`: passed.
- `just guardrails`: passed.
- Primary LSP checks for nine documentation files and the relevant lifecycle source files: passed.
- Relative link file-existence check: 21 targets, no missing files.
- STE lint: completed with advisory style counts, not a correctness verdict.
- `git diff --check`: passed.

The link check did not validate anchors or remote URLs.
Later R30–R35 work supplies executable onboarding, behavioral documentation gates, and the strict site build. Independent M6 acceptance is now complete.
No application code, protobuf identity, SQL migration, public declaration, or guardrail budget changed for R28.
The later review required default unknown-outcome blocking to be distinguished from checked, sink-guaranteed bounded retries.
The correction and executable retry-policy regression are recorded in the [review follow-up](m6-contract-site-validation.md#independent-review-and-corrections).

## R29: Real daemon configuration and library options

Corrections cover `docs/GRPC_EXECUTION.md`, `docs/RUNTIME_CONFIG.md`, `docs/COMMANDS.md`, and `docs/CLIENT_EXAMPLES.md`.
The guides no longer present a YAML runtime configuration or a daemon authentication-disable option.
They separate daemon flags and environment variables from Go library authentication, transport, and limit options.

The command reference includes all registered daemon flags and their defaults, including mode selection and HTTP shutdown grace.
A new regression compares the documented table with the executable's registered flags and default values.
It does not compare against a second hand-maintained flag inventory.

The gRPC guide states disabled-by-default service binding, required bearer authentication, TLS policy, fixed daemon limits, and library-only overrides.
Client guidance distinguishes terminal failure details from admission and distinguishes transient dependency failures from business failures.
The current Kafka cluster namespace remains fixed to `default`. Different broker lists do not create separate identity namespaces automatically.

Validation in `out/remediation/m6-r29-results.json`:

- Runtime and daemon configuration, constructor, authentication, mode, CLI, gRPC, and limit tests under `-race -count=3`: passed.
- `TestDocumentedDaemonFlagNamesAndDefaults`: passed against the actual registered flag set.
- `just guardrails`: passed, including the new documented-flag regression.
- Primary LSP checks for the new test and configuration docs: passed without errors.

The later R33 gate supplies live authenticated Go/Python onboarding and separate TLS verification.
The later R34–R35 gates add behavioral contracts and strict whole-site validation.
The completed review receipt accepts R29 and R34 separately. The later correction review accepts R28, R35, and combined M6 consistency.
Neither the focused R29 tests nor the in-memory R33 fixture establish a complete daemon deployment.

## Accepted snapshot continuity

The original M5 and R30–R33 snapshots remain historical acceptance evidence.
Later R34–R35 work amends contributor guidance, indexes, and documentation tests; it does not rewrite those preserved snapshots.
R33's original reconstruction now has an exact archive and a hidden evidence directory so Go does not discover it as another package.
The location mapping is recorded in the [contracts and site evidence](m6-contract-site-validation.md).
