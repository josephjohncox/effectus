# Codebase remediation checklist

## Objective

Resolve the findings in [the review at 388c3cb](audits/codebase-review-388c3cb.md).
Keep the review as historical evidence. Use this checklist as the current source of work status.

## Current status

R01–R40 are implemented and independently accepted for the recorded remediation scope.
[PR #74](https://github.com/josephjohncox/effectus/pull/74) merged that work into `main`.
The subsequent [v0.5.0 release](https://github.com/josephjohncox/effectus/releases/tag/v0.5.0)
passed CI, documentation, and publication workflows at `d14b94b`.
See the [repository and PR audit](audits/repository-state-2026-09-12.md) for branch reconciliation and dependency follow-ups.
Deployed Kubernetes recovery and representative production-capacity validation remain open.

## Original remediation execution rules (historical)

These rules record the original implementation authorization. Later publication, PR, and release work had separate authorization.

1. Read the relevant finding and source before each change.
2. Reproduce correctness findings with regression tests when practical.
3. Fix the smallest complete contract, not only the reported line.
4. Keep authentication, fencing, idempotency, and immutable artifact checks fail-closed.
5. Preserve existing wire field numbers and supported Go imports. Document intentional behavior changes.
6. Run focused tests after each change. Run independent review after each milestone.
7. Mark a task complete only when its acceptance criteria have evidence.
8. Record skipped checks and blockers. An unavailable integration fixture is not a passing test.
9. Preserve the pre-existing `.github/scripts/release-preflight.sh` change. Do not commit, reset, or overwrite it.
10. Do not publish, push, deploy, delete data, or change unrelated configuration.
11. Do not weaken guardrails, remove tests, or inflate budgets merely to make checks pass.
12. Keep one writer in the checkout. Reviewers are read-only.

Source changes may remain uncommitted for user review. This task does not authorize a release or destructive database operations.

## Design decisions

- **Unsupported functions:** reject unavailable predicate functions before runtime startup unless a complete immutable implementation already exists.
  Do not add arbitrary callbacks to production generations to satisfy F01.
- **Collections:** support the checker-defined closed list/map/object types at admission, including nested values and invalid-element rejection.
- **Value representations (M1 approval):** integers are signed int64; floats are finite float64; bytes accept raw `[]byte` or canonical padded base64 and normalize to a JSON-safe base64 string. Recursively copy closed values and reject invalid elements, cycles/excess depth, overflow and malformed base64. Preserve exact JSON integer parsing; protobuf `Struct` doubles cannot transport every int64 losslessly.
- **Integer source spelling (M1 approval):** the underlying parser rejects direct `-9223372036854775808`; use the tested `(-9223372036854775807 - 1)` expression. This syntax limitation does not narrow the supported fact/IR integer value range. Do not preprocess source strings or fork the parser in this milestone.
- **Defaults compatibility (M1 approval):** retain deprecated `ir.DefaultLimits` but never read it internally; `ir.ValidationDefaults()` returns independent copies. The repository's 2027-09-01 deprecation horizon requires compatibility review and an approved breaking release before removal; this remediation authorizes neither removal nor release.
- **Results:** durable acceptance and terminal completion are different outcomes.
  Admission-only replay may return the existing identity without a business error.
  Terminal replay must preserve failure/blocked disposition, and gRPC `success` must not mean only accepted.
- **Recovery:** expired leases must not authorize writes or external effects.
  Prefer bounded acquisition and explicit lease maintenance over a large serial batch with a fixed deadline.
- **Public API:** preserve source/wire compatibility where practical through deprecated wrappers and additive fields.
  Document reserved services rather than delete or renumber existing protobuf fields.
- **Namespaces:** require explicit namespaces on supported new-client paths. Keep any necessary legacy alias explicit and tested.
- **Documentation:** normative pages describe implemented behavior. Mark history and theory clearly.
- **Evidence:** replace baseline scores only after final validation. Local benchmarks do not establish production capacity.

## Milestone 1: Executable language contract

- [x] **R01 / F01:** Reject unavailable function calls before a generation serves traffic, or implement a complete immutable function contract.
  Acceptance: compiler/generation regression, clear diagnostic, and no compiler-approved unsupported runtime call.
- [x] **R02 / F02:** Align collection admission with IR types.
  Acceptance: generic/named lists and maps, nested objects, invalid elements, scalar boundaries, serialization, and dry-run/admission tests.
- [x] **R03 / F20:** Make validator defaults internally immutable.
  Acceptance: zero-valued checks ignore caller mutation of deprecated globals, copied defaults are safe, and negative limits fail explicitly.
- [x] **R04 / F30:** Add cross-layer language conformance coverage.
  Acceptance: supported expressions/types compile, check, dry-run, admit, execute, and survive checked serialization as applicable.

Implementation and independent acceptance evidence for R01-R04 is in [M1 validation](audits/remediation-validation.md#m1-r01-r04-executable-language-contract). No external integration gate is implied by these language checks.

## Milestone 2: Durable dispatch and recovery

- [x] **R05 / F03:** Persist post-invocation disposition despite caller cancellation, with a bounded independent context and valid lease authority.
  Acceptance: cancel-after-commit, canceled unknown outcome, failed completion persistence, and expired-lease tests.
- [x] **R06 / F04:** Prevent recovery leases from expiring unused and maintain authority for long executions.
  Acceptance: slow batch, long execution, competing worker, lease-loss, cancellation, and terminal-write CAS tests.
- [x] **R07 / F20:** Reject invalid dispatch/recovery durations, counts, and retry settings.
  Acceptance: table-driven negative/zero/valid setting tests and documented defaults.
- [x] **R08 / F30:** Extend durable fault-injection and integration regressions.
  Acceptance: tests distinguish known-not-committed, success, unknown, stale fencing, exhausted retry, and terminal business failure.

Implementation, PostgreSQL validation, and independent acceptance evidence is in [M2 validation](audits/remediation-validation.md#m2-r05-r08-durable-dispatch-and-recovery). That section corrects the historical audit's expiry-only CAS claim and records non-blocking test gaps.

## Milestone 3: Engine identity, replay, and ownership

- [x] **R09 / F05:** Preserve terminal failure/blocked semantics on replay while retaining explicit admission-only behavior.
  Acceptance: first attempt and replay tests for completed, failed, and every blocked state, with typed errors for terminal callers.
- [x] **R10 / F09:** Verify ruleset/version against immutable generation identity before durable writes.
  Acceptance: mismatches invoke no executor and create no mislabeled record. Matching historical replay remains well-defined.
- [x] **R11 / F10:** Normalize request defaults before hashing without mutating caller input.
  Acceptance: omitted/explicit merge equivalence, genuine conflict, nested fact identity, and input-ownership tests.
- [x] **R12 / F11:** Refresh cached execution state from durable records and resolve optimistic conflicts safely.
  Acceptance: two-engine replay/recovery tests, newer terminal state visibility, race tests, and no stale revision loop.
- [x] **R13 / F12, F31:** Track historical generation ownership and bounded execution-cache retention.
  Acceptance: duplicate resolution, concurrent use, historical completion, close-after-use, failed resolution, exactly-once closer, and cache-bound tests.

Implementation, compatibility, test, and independent acceptance evidence is in [M3 validation](audits/remediation-validation.md#m3-r09-r13-identity-replay-and-ownership). Transport error mapping and daemon shutdown remain separate requirements below.

## Milestone 4: Transport, executor, and CLI contracts

- [x] **R14 / F05, F15:** Add unambiguous gRPC admission/completion/state semantics without breaking existing wire identities.
  Acceptance: generated bindings updated, success reflects completion, typed state exposed, failure replay and wait-mode tests.
- [x] **R15 / F06:** Stop admission and drain HTTP before dependency closure. Clean up partial startup failures and join workers.
  Acceptance: active-handler shutdown, deadline expiry, service failure, and resource-close ordering tests.
- [x] **R16 / F07:** Apply typed, sanitized HTTP error mapping consistent with gRPC.
  Acceptance: invalid input, conflicts, storage outage, internal failure, cancellation, and retryable dependency tests.
- [x] **R17 / F08, F26:** Enforce overflow-aware JSON limits and exact route/method contracts.
  Acceptance: all routes, chunked/exact/oversize bodies, unknown fields, trailing data, auth, status codes, and response shapes.
- [x] **R18 / F13:** Require EOF after an executor result and classify malformed responses conservatively.
  Acceptance: trailing junk, multiple values, empty body, oversize response, and normal result tests.
- [x] **R19 / F14:** Verify canonical argument hashes before business invocation.
  Acceptance: mismatches do not call the handler, equivalent JSON hashes match, and duplicate identity semantics remain intact.
- [x] **R20 / F16:** Make the gRPC constructor contract honest and validate before opening listeners.
  Acceptance: required options and safe defaults tested, no bind side effect on invalid configuration, documented usable constructor.
- [x] **R21 / F17:** Align namespace requirements across supported transports and document retained aliases.
  Acceptance: blank/whitespace namespace, legacy universe alias, and identical logical identity tests.
- [x] **R22 / F18:** Correct command-specific flags, successful help, explicit daemon modes, and positional argument validation.
  Acceptance: executable CLI tests for accepted/rejected combinations and exit codes.
- [x] **R23 / F18:** Make compiler writes atomic and prevent input/output alias destruction.
  Acceptance: same path, symlink/hardlink aliases, existing output, write/rename failure cleanup, and successful deterministic output tests.
- [x] **R24 / F20:** Apply consistent negative-limit validation to HTTP invocation and remaining transport options.
  Acceptance: documented zero defaults, rejected negatives, and no silently relaxed safety limits.

Implementation, compatibility, tests, and independent acceptance are recorded in [M4 transport validation](audits/m4-transport-validation.md). Shutdown remains cooperative.

## Milestone 5: Supported public API

R25–R27 passed independent acceptance. See [M5 API validation](audits/m5-api-validation.md) for source, tests, and exact snapshot reconciliation.

- [x] **R25 / F15:** Freeze and label reserved/unsupported protobuf capabilities.
  Acceptance: complete service capability matrix, unsupported-RPC tests, deprecation comments, and compatibility checks.
- [x] **R26 / F19:** Harden or safely deprecate the exported BufIntegration compatibility surface.
  Acceptance: nil/canceled input, defensive copy, concurrency, configured paths, and no hidden constructor filesystem mutation tests.
- [x] **R27 / F21:** Document the supported Go API and narrow the recommended entry path without breaking imports.
  Acceptance: package guides and useful export comments cover ownership, concurrency, contexts, errors, state machines, and compatibility aliases.
  Audit the public surface and report unsupported exports rather than describe them as supported features.

## Milestone 6: Documentation and executable onboarding

R28–R35 and combined M6 passed independent acceptance after the unknown-outcome and historical-index corrections. See [M6 documentation evidence](audits/m6-documentation-validation.md).

- [x] **R28 / F22:** Remove contradictory lifecycle claims from current architecture, intent, coherent-flow, and theory pages.
  Acceptance: all normative pages match startup compilation, process replacement, recovery, and tested shutdown behavior.
- [x] **R29 / F23:** Replace fictional gRPC/YAML configuration with actual flags, environment variables, and separate library options.
  Acceptance: tested commands, authentication/TLS instructions, default limits, and inbound/outbound distinctions.
- [x] **R30 / F24:** Write an incremental concepts and integration tutorial with `.eff` and `.effx` paths.
  Acceptance: executable snippets teach facts, contracts, verbs, bindings, ordering, bundles, resolvers, diagnostics, and outcomes.
- [x] **R31 / F25:** Correct contributor and agent commands, adding missing recipes only when they provide a supported workflow.
  Acceptance: documented Just recipes exist and SQL/protobuf regeneration instructions match the repository.
- [x] **R32 / F26:** Publish a complete HTTP API reference.
  Acceptance: routes, methods, auth, request/response schemas, body limits, errors, aliases, idempotency, and wait semantics match tests.
- [x] **R33 / F27:** Execute authenticated Go gRPC and Python examples and test TLS separately.
  Acceptance: runnable matching service, authentication/idempotency checks, clear prerequisites, and CI or equivalent automated gate.
- [x] **R34 / F28:** Replace lexical-only documentation checks with behavioral contracts.
  Acceptance: per-command help, defaults/required combinations, recipes, snippets, JSON contracts, local links, and stale-claim checks.
- [x] **R35 / F29:** Repair documentation indexes, glossary, release prerequisites, and historical/current distinctions.
  Acceptance: no remaining unsupported current-product claims and strict documentation build.

## Milestone 7: Measurements and completion audit

Current [M7 measurement evidence](audits/m7-measurement-validation.md) records accepted coverage interpretation and two accepted regression additions.
The parent also passed recovery tests with independently offset PostgreSQL process clocks at ±24 hours.
Independent review accepted the clock-specific work and whole-task R36 with no findings.
Independent review accepted the 28 [R37 benchmark cases and bounded measurements](audits/m7-benchmark-validation.md) with no findings.
Independent review accepted [R38 local validation and remaining external gates](audits/m7-full-validation.md#independent-r38-acceptance) after the Helm metadata correction.
The first R39 boundary/API and usability reviews passed. The core review required three P1 corrections.
The first correction review accepted C1, C3, and C2's resume path, but required an admission-replay cancellation follow-up.
The parent reproduced and corrected that path, then passed targeted races, full-root races, vet, lint, and guardrails.
Independent re-review accepted [the final R39 diff and R40 finding reconciliation](audits/m7-final-review.md#independent-final-acceptance) with no findings.
The unavailable external gates remain unchecked.
The separately authorized [standalone Compose follow-up](audits/standalone-compose-validation.md#independent-acceptance) passed independent acceptance for its bounded local scope.
The separately authorized [Kafka-to-business-commit follow-up](audits/kafka-business-commit-validation.md#independent-acceptance) also passed independent acceptance for its bounded local scope.
The reviewed source was then published to a validation branch, where [remote CI passed all 15 jobs](audits/remote-ci-validation.md) after two dependency advisory fixes.
Release publication subsequently passed for v0.5.0. The two remaining external gates need a Kubernetes recovery environment and representative capacity measurements; publication alone does not close either gate.

- [x] **R36 / F30:** Measure meaningful coverage and close remaining regression gaps.
  Acceptance: report exact commands, exclusions, cross-package coverage limitations, changed-behavior tests, race results, and remaining blind spots.
- [x] **R37 / F31:** Add and run representative benchmarks.
  Acceptance: compilation, checking, admission/evaluation, recovery, and dispatch fixtures with allocations, repeat counts, toolchain, machine context, and bounds.
- [x] **R38 / F32:** Run full available validation, including PostgreSQL, Kafka, examples, documentation, protobuf compatibility, and tools.
  Acceptance: record actual results. Leave unavailable external gates unchecked with exact setup requirements and blockers.
- [x] **R39 / F32:** Complete independent correctness and usability review, resolve accepted findings, and review the final diff.
  Acceptance: review evidence tied to current files, no unresolved blocker, no user-file damage, and focused reruns after fixes.
- [x] **R40 / all:** Reconcile every finding with code, documentation, tests, and final status.
  Acceptance: durable validation report, explicit behavior changes and residual risks, accurate checklist, and no claim of completion for blocked work.

## Execution log (historical)

### Baseline recorded

- Audit preserved at `docs/audits/codebase-review-388c3cb.md`.
- Initial repository: `main` at `388c3cb23943fc9032bcf37a18277688596972d6`.
- Pre-existing user edit: `.github/scripts/release-preflight.sh`, 8 insertions and 8 deletions.
- Previous audit tests passed, but they are baseline evidence, not validation of the coming changes.
- Plan: seven serial milestones with one writer, focused regression tests, and fresh read-only review between milestones.
- Status: **blocked before implementation**. All R01-R40 tasks remain unchecked.

### Background runner failed before the first worker started

Workflow `580c08bf-bbc1-40b3-bf5b-37880e85adcb` failed at `language-implement` on 2026-09-07.
Pi could not resolve its required server/client modules. No child session was persisted and no implementation ran.
The parent verified that HEAD, the user-edited release script, and the unstaged source state were preserved.

See [B01 and repository verification](audits/remediation-validation.md) for the exact error, run IDs, file hash, and recovery options.
The parent did not modify the global Pi installation or switch execution modes.
The user subsequently upgraded and restarted Pi, then authorized a retry and direct-session implementation if the runner still fails.
Before retry, the parent confirmed the same HEAD, no staged changes, no worker source changes, and the preserved release-script hash.
Pi's doctor now reports async support available. Actual child launch remains the verification step.
Next action: relaunch the saved workflow from R01. If startup fails again, record the error and continue directly under that authorization.

### Milestone 1 implementation checkpoint (historical)

The retried child writer started successfully and implemented R01-R04 on the shared checkout; no execution-mode fallback was needed. No previous milestone review was supplied.

- Reproduced unavailable functions, generic/named collection rejection, JSON bytes rejection, integer overflow acceptance, numeric comparison/equality drift, mutable defaults and negative-bound defaulting against a temporary `git archive` of the initial HEAD. Expected-failure details are preserved in the validation report; the main checkout was not reset.
- Compiler and generation construction now reject unavailable calls, including unreachable branches. Generic IR retains pure/total function checking, but cannot bypass generation checks.
- Shared closed-value normalization aligns dry-run, admission, frozen JSON facts, execution and result-slot consumption. Added `.eff`/`.effx`, generic IR literal, scalar-boundary, empty/nested collection, invalid-result and immutable-default race regressions.
- Source parsing of the direct minimum-int64 decimal literal remains intentionally rejected with a workaround diagnostic; its arithmetic spelling, fact value and IR literal pass end-to-end tests.
- Focused Go tests, focused races, ten repeated shared-default race runs, affected examples, `go vet`, full ordinary `go test -count=1 ./...`, and repository guardrails passed. `just test-examples` reported completed embedded execution/replay and `review_count:1`; its gRPC package has no executable test. Full-suite ordinary tests preceded the final minimum-literal diagnostic/test addition; focused tests/races/vet/guardrails were rerun afterward.
- Manually updated only the approved IR inventory entries: additive `NormalizeValue`, additive `ValidationDefaults`, and `DefaultLimits`' initializer. No budget regeneration or guardrail weakening.
- R11 remains open: declared-type normalization before request hashing needs explicit historical replay compatibility for numeric lexical forms, bytes/list representations, nil collections, omitted merge policy and nested/dotted fact identity. M1 preserves baseline-supported hash goldens rather than silently migrating identities.
- No PostgreSQL/Kafka integration, authenticated gRPC/Python demonstration, final coverage/benchmark gate or independent review is claimed. Release-script SHA256 remains `3ee2921f9f14c5f32ad73512cf2ea718ea897ffb9037c963cc214b5386a7599b`; no files are staged and no commits were made.

## Evidence and blockers

Current evidence: [remediation validation and blockers](audits/remediation-validation.md).
For each later result, include task IDs, changed behavior, tests and their outcomes, review disposition, and remaining work.

R01–R40 are implemented and independently accepted. See the [final review and finding reconciliation](audits/m7-final-review.md#independent-final-acceptance) and [repository and PR audit](audits/repository-state-2026-09-12.md). Deployed Kubernetes recovery and production-capacity validation remain open; the recorded checks do not establish production readiness.
