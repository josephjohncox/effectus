# Remediation validation and blockers

## Current status

**R01–R40 independently accepted for local remediation scope. External gates remain open.**
The first correction review accepted C1, C3, and C2's resume path, but found a remaining admission-replay cancellation defect.
The parent reproduced and corrected that follow-up, then passed targeted and full-root races, vet, lint, and guardrails.
See [the corrections and complete finding reconciliation](m7-final-review.md).
The M1–M3 sections below are historical milestone records, not current unchecked-task lists.
The parent continued after the earlier writer timeout under prior user authorization. See B02 below.
R14–R24 passed independent acceptance after the storage-error classification fix. See [M4 transport validation](m4-transport-validation.md).
Combined M5 review accepted R25–R27 with a formatting-only snapshot note, now reconciled.
R30–R31 also passed independent acceptance, including the verified formatting-only snapshot amendment. See [tutorial and contributor validation](m6-tutorial-contributor-validation.md).
R33 passed independent acceptance for live authenticated Go/Python clients and separate TLS checks. See [client validation](m6-grpc-client-validation.md).
R32 passed independent acceptance after its header-normalization wording correction and raw TCP regression. See [HTTP reference validation](m6-http-reference-validation.md).
The completed M6 review receipt accepts R29 and R34 independently. See [the review and correction evidence](m6-contract-site-validation.md#independent-review-and-corrections).
The correction re-review accepts R28, R35, and combined M6 with no findings. See [M6 acceptance](m6-contract-site-validation.md#combined-m6-acceptance).
The final R36 review accepts the clock-specific work and combined measurement/regression scope with no findings. See [M7 measurement evidence](m7-measurement-validation.md).
R37 also passed independent acceptance with no findings. See [benchmark measurements and acceptance](m7-benchmark-validation.md).
R38 passed independent acceptance after the Helm version-evidence correction. See [R38 acceptance](m7-full-validation.md#independent-r38-acceptance).
The first R39 boundary/API and usability reviews passed for their frozen lanes.
Independent re-review accepted the final R39 corrections and R40 reconciliation with no findings.
See [the acceptance and frozen evidence](m7-final-review.md#independent-final-acceptance) and [the completed local checklist](../REMEDIATION.md).
See [M5 API validation](m5-api-validation.md) for the accepted source snapshot and review evidence.
The isolated PostgreSQL fixture passes the M2–M4 integration suites, including race builds.
R38 parent execution passed real Kafka commit/restart tests, declared-toolchain Go races, and additional tool checks. See [available validation and external gates](m7-full-validation.md).
R38 acceptance leaves its unavailable external gates unchecked. R39/R40 local acceptance cannot establish production readiness.
The user separately authorized a [retention-safe standalone Compose follow-up](standalone-compose-validation.md#independent-acceptance).
Independent review accepted its bounded first-run/restart scope and evidence reconciliation with no findings.
That acceptance closed only the standalone Compose gate.
The user then authorized a separate [Kafka-to-business-commit fixture](kafka-business-commit-validation.md#independent-acceptance).
Independent review accepted its bounded commit-window, deduplication, Kafka-redelivery, and retained-stack restart evidence with no findings.
The combined Kafka gate also closes. The other three external gates remain open.
All ten earlier containers remained unchanged, and all six new containers remain retained.
The earlier six fixtures remained unchanged, and the new stack remains retained.
Neither the broker test nor the accepted Python/TLS fixture establishes a durable daemon deployment.
The user authorized a Git checkpoint before work resumes on the remaining milestones.
The checkpoint includes accepted R01–R24, implemented R25, and partial R26. It is not final M5 acceptance.
Full repository race tests, PostgreSQL race integration, and guardrails passed again before staging.
Their logs and exit records are under `out/remediation/checkpoint-*` and `checkpoint-results.json`.
The pre-existing release-script edit is excluded from the checkpoint.
Commit `673d6c9d87c5aa69ce88306ca424c6788d9e3de2` was pushed to `origin/main` and verified against the remote.
Subsequent remediation work, including the accepted R39 corrections and R40 reconciliation, remains local and unstaged.
The first R39 corrections passed full-root and corrected PostgreSQL integration races, vet, lint, and guardrails.
The later admission-replay fix passed new targeted/full-root races, vet, lint, and guardrails. Earlier integration evidence keeps its original chronology.
The protected release-script hash remains unchanged.

## M1: R01-R04 executable language contract

### Reproduction and disposition

The historical audit was verified against a temporary archive, not by resetting the working checkout:

```bash
baseline=$(mktemp -d /tmp/effectus-language-baseline.XXXXXX)
git archive 388c3cb23943fc9032bcf37a18277688596972d6 | tar -x -C "$baseline"
# Add focused regression fixtures using only the baseline APIs.
(cd "$baseline" && go test -count=1 -run TestRemediationBaseline ./compiler ./ir ./runtime)
```

The actual archive was `/tmp/effectus-language-baseline.9fwOHd`. The command failed as expected:

- Compiler accepted declared pure `lower(order.id)` although no generation implementation exists.
- Generic `list<string>` and `map<int>` returned `unknown checked type`; named `Tags`/`Counts` returned incompatible value errors.
- Canonical JSON bytes `"AP8="` failed bytes admission; `uint64(MaxInt64)+1` was accepted as an integer.
- Equality of `json.Number("1")` and `float64(1)` was false. Comparison of int64 `9007199254740993` with float64 `9007199254740992` incorrectly reported not greater.
- `MaxDepth:-1` silently defaulted. Mutating `DefaultLimits.MaxArtifactBytes=1` made zero-valued checking reject a 659-byte valid artifact.

The permanent regressions are in `compiler/checked_test.go`, `ir/types_test.go`, `ir/value_test.go` and `runtime/language_conformance_test.go`. Baseline hash goldens were additionally run in that archive with `go test -count=1 -run TestRemediationBaselineHashGoldens ./runtime` and **passed**; the same goldens pass in the current checkout. Temporary fixture files are supporting diagnostics, not required test dependencies.

### Implemented feature contract and approved clarifications

**R01 / functions.** `CompileChecked` rejects all named/builtin predicate calls with the function name and an immutable-generation diagnostic. Both dialects and unreachable calls are covered. `NewGeneration` independently traverses every predicate branch and rejects function calls for production and non-production generations, even with claimed function IDs. Generic `ir.Check`/`ir.Parse` retain declared pure/total function checking for lower-level consumers; that is not a runtime implementation or an arbitrary callback API. Existing generic function coverage remains.

**R02 / closed values.** `ir.NormalizeValue` shares the checker's type parser/resolver, copies values, and validates `list<T>`, `[]T`, `map<T>`, named lists/maps and named objects recursively. Lists accept Go slices/arrays, maps require string keys, objects reject unknown fields and enforce required fields. Errors identify list indices and nested field paths. Reference assignability now also rejects extra object fields and optional-to-required field promises; object literals and references obey the same closed contract. Unknown/open types remain rejected.

The supervisor explicitly approved these representation clarifications; no protobuf or wire fields changed:

- Integer values are signed int64. `json.Number` integer validation is exact, including integral decimal/exponent forms, without a float64 intermediate. Fractions, neighboring out-of-range values, non-finite inputs and float64 rounding to the exclusive `2^63` upper boundary fail. Finite integral Go float inputs within range are accepted as their already-represented value.
- Float values are finite float64 and may round inputs to float64 precision. Numeric equality, nested collection equality/membership and ordering compare numbers consistently; int/float comparisons above `2^53` do not round the integer to a double first. Integer arithmetic/negation overflow and non-finite arithmetic results fail rather than wrap.
- Bytes accept `[]byte` or **canonical padded standard base64**, normalize to a base64 string, and reject unpadded/noncanonical encodings, nonzero padding bits and embedded newlines. Nil/empty raw bytes normalize to `""`. Predicate literals, fact operands, list membership, first-step arguments, subsequent arguments and result-slot values retain byte semantics through JSON/artifact round trips.
- Null is only a value of `null`, not a nullable wrapper for other types. Empty/typed-nil lists and maps normalize to `[]`/`{}`. Named and generic collections stay closed when empty. A raw `[]byte` supplied to a declared integer list normalizes as list elements; supplied to `bytes`, it normalizes as base64.
- Value normalization rejects depth over 64, more than 10,000 visited value nodes/items, malformed type definitions, non-string map keys and cyclic Go values through bounded failure. Generic type nesting is also bounded at 64. Exact JSON-number parsing bounds text to 1,024 bytes and exponent magnitude to 400 before rational allocation. These are fixed runtime value safety bounds, not a new transport-options API.
- Dry-run and admission share normalization and owned JSON snapshots. Result values are validated before binding to downstream result slots; a bad result cannot reach the next executor. This does not undo an already committed producer effect or redefine durable disposition/replay (later milestones).

**Signed minimum source spelling limitation (approved).** The underlying expr parser rejects `-9223372036854775808` while parsing its positive magnitude. The compiler now provides a useful range diagnostic and the tested workaround `(-9223372036854775807 - 1)`. That expression executes with integer arithmetic, not a double intermediate. MinInt64/MaxInt64 facts, direct MinInt64 IR literals, JSON/protobuf round trips, equality, comparison and execution pass. Adjacent invalid integers fail. No token rewriting, dependency change or parser fork was introduced. The supported integer **value** range is full signed int64; this particular direct decimal **spelling** is not supported. The documentation stage must state this distinction in the language reference/tutorial.

**R03 / defaults.** `ir.ValidationDefaults()` returns copies of fixed bounds. Internal `Check` and `Parse` never read deprecated `DefaultLimits`; a concurrent writer to that compatibility variable cannot change checking or race with internal reads. Every negative `Limits` field is rejected with its name and `ErrInvalidArtifact`; zero retains the fixed default. Per supervisor approval, the existing repository deprecation horizon is `2027-09-01`, conditional on compatibility review and an approved breaking release. This work does not remove the symbol or authorize a release.

**R04 / conformance.** Permanent tests exercise Boolean operators/short circuit, comparisons, equality, string concatenation/search/regex, integer/float arithmetic and negation, list membership/literals, null, generic/named collections, nested objects, bytes, empty collections, large integers, `.eff` rules and `.effx` result-binding flows. They compile, recheck, serialize/reparse, dry-run, durably admit into in-memory stores, resume on a fresh engine, and execute. Frozen JSON facts are re-evaluated rather than merely trusting pinned selected IDs. Unsupported functions, invalid collection elements/results and scalar bounds fail closed. Generic IR byte/integer literals cover representations without source syntax.

### Identity compatibility at the M1 boundary

The admission hash's generic normalizer now accepts JSON-compatible typed collections/bytes, rejects cycles/excessive work, and does not call caller-supplied marshalers. Fact-application JSON uses this same safe canonicalizer instead of invoking marshalers on the raw input. Previously supported nil `map[string]any`/`[]any` values still hash as `{}`/`[]`, including nested cases; null remains null. Golden hashes also cover empty collections, normal maps, MaxInt64 and numeric lexical forms. No hash migration was silently introduced.

**Historical M1 status; resolved by M3 below:** request hashing still preceded declared-type normalization. It must explicitly reconcile omitted/explicit merge policy, `1`/`1.0`/`1e0`, typed bytes versus base64 strings, `[]byte` interpreted as a declared integer list versus an ordinary numeric slice, nil collection representations, nested/dotted fact collisions and caller ownership. Historical replay identities require compatibility handling, not simply replacing the current hash. M1 pins existing supported hash goldens and records this dependency.

`google.protobuf.Struct` carries doubles, so it cannot preserve all int64 digits. JSON parsing with `UseNumber` and the embedded API can preserve exact integers, but digits already rounded by a caller or a Struct transport cannot be recovered. This milestone does not claim full-int64 lossless gRPC Struct transport; transport/tutorial documentation must state the limitation.

### Validation actually run

Toolchain: `go version go1.26.7 linux/arm64`.

| Command | Observed result |
| --- | --- |
| `go test -count=1 ./compiler ./ir ./runtime ./examples/...` | Passed after final code changes; embedded example tests passed; other three example packages report no test files. |
| `go test -race -count=1 ./compiler ./ir ./runtime ./examples/...` | Passed after final code changes; runtime 1.430s, IR 1.127s, compiler 1.140s on this run. |
| `go test -race -count=10 -run TestValidationDefaultsIgnoreGlobalMutationAndReturnCopies ./ir` | Passed, 1.843s; shared-default writer/checker test repeated ten times. |
| `go vet ./compiler ./ir ./runtime ./examples/...` | Passed after final code changes, no diagnostics. |
| `go test -count=1 ./schema ./bundle ./embedded` | Passed; embedded library has no package-local tests. |
| `go test -count=1 ./...` | Passed before the final minimum-int64 spelling diagnostic/tests; focused suites/races/vet were rerun after those additions. Includes generated and a discovered node_modules Go package; not a handwritten-code coverage metric. |
| `just test-examples` | Passed: orderreview tests, embedded executable returned completed/replayed identity and `review_count:1`; gRPC example only compiled (`[no test files]`). |
| `go run ./internal/guardrails/cmd check` | Passed after final code changes (`repository guardrails passed`). |
| `git diff --check` | Passed. |

Initial new fixture runs failed because the synthetic generation lacked its required source digest; the fixture was corrected rather than relaxing artifact checks. The direct MinInt64 spelling regression then exposed the parser limit above; its approved rejection/workaround tests pass. A mistaken guardrail CLI `--help` invocation returned its documented-command error; the actual `check` command passed.

The child did not run PostgreSQL/Kafka integration, live authenticated gRPC/Python examples, TLS gates, full coverage measurements, benchmarks, or independent review in M1. After recovery, the parent reran focused tests and race tests and obtained an independent read-only review that accepted R01-R04 without required fixes. In-memory durable tests establish language semantics, not database isolation or destination-level exactly-once behavior. R33/R36-R39 retain those gates; no unavailable integration is marked passing.

### Surface, ownership and review disposition

Changed implementation/test files: `compiler/checked.go`, `compiler/checked_test.go`, `ir/check.go`, `ir/types.go`, `ir/types_test.go`, new `ir/value.go`, new `ir/value_test.go`, `runtime/checked_workflow.go`, `runtime/durable_admission.go`, `runtime/engine.go`, `runtime/generation.go`, `runtime/generation_view.go`, and new `runtime/language_conformance_test.go`. Updated this evidence, `docs/REMEDIATION.md` and the specific `guardrails/public-api.txt` entries.

The only approved public additions are `ir.NormalizeValue(Environment, string, any) (any, error)` and `ir.ValidationDefaults() Limits`; the retained `DefaultLimits` initializer changes to the copy-returning function. The inventory was edited for exactly those three entries, not regenerated; no budgets or dependency rules were weakened. Supported Go import paths and protobuf field identities are unchanged.

**Independent M1 review accepted R01-R04 without required fixes.** The parent also reran `go test -count=1 ./compiler ./ir ./runtime` and its `-race` counterpart after the timeout. Logs are `out/remediation/m1-focused.log` and `m1-race.log`. Remaining milestones are not complete.

The user-owned `.github/scripts/release-preflight.sh` was never edited and retains SHA256 `3ee2921f9f14c5f32ad73512cf2ea718ea897ffb9037c963cc214b5386a7599b`. `git diff --cached --name-only` is empty. No commits, resets, pushes, deployments or destructive data operations were performed. Baseline reproduction used only a temporary archive outside the checkout.

## M2: R05-R08 durable dispatch and recovery

### Behavior and evidence

- **R05:** Destination outcomes survive caller cancellation. Completion uses a cancellation-independent context capped at five seconds and the remaining dispatch lease. The engine also gives post-workflow disposition a five-second independent context. Invalid or unserializable outcomes become blocked unknown, not implicit retries. A storage failure remains an explicit failure; it is not reported as a successful durable write.
- **R06:** Recovery acquires one execution at a time, not an idle leased batch. The optional `ledger.ExecutionLeaseRenewer` interface permits long-running work without breaking existing `ExecutionLedger` implementations. Both built-in stores renew only a current, unexpired owner/token/revision and never shorten its deadline. Renewal keeps the revision stable so completion can use the original handle. Completion now rejects expiry even before another worker claims the execution. Stores without renewal are restricted to the original lease's safe execution window.
- **R07:** Negative dispatch/recovery settings fail explicitly. Zero values retain documented defaults. Explicit invocation timeout must be shorter than the dispatch lease; initial backoff must not exceed its maximum. Stable jitter is capped after multiplication and cannot overflow that maximum. Tests cover defaults, valid values, negatives, invalid combinations, and extreme attempt counts.
- **R08:** Tests cover cancellation after success, canceled unknown/permanent outcomes, failed completion writes, malformed successful results, expiry, replacement, long execution, slow serial batches, renewal loss, and completion during an in-flight renewal. Existing fault-injection and PostgreSQL tests also passed for known-not-committed, unknown, fencing rejection, retry exhaustion, and terminal business failure.

Scheduling uses local monotonic durations measured **before** claim/renewal RPCs. Returned database wall-clock timestamps do not control invocation or heartbeat timers. The database clock remains authoritative for renewal and completion CAS. A synchronous renewal confirms authority before workflow execution. Renewal loss cancels ongoing work and ends that poll instead of immediately invoking the same execution again. Renewal goroutines are canceled and joined.

**Audit correction:** the original `FinishExecutionLease` implementations checked owner/token/revision, but did not reject deadline expiry alone. The historical F04 discussion overstated that check. The new explicit expiry condition closes that gap; an unreplaced expired handle can no longer finalize work.

### Independent review

The first read-only review required three fixes: confirm lease authority before execution, stop using database wall timestamps for local scheduling, and stop same-poll repeated invocation after renewal failure. All three were implemented. Added regressions cover replacement before start with zero invocations, reported clock skew of ±24 hours, renewal failure with `BatchSize=8`, and a blocked renewal joined during successful completion. A second independent read-only review **accepted R05-R08 with no blocking findings**.

Remaining non-blocking test gaps: direct PostgreSQL renewal-versus-finish concurrency, a focused delayed-claim test for custom stores without renewal, and testing a genuinely skewed database clock rather than altered returned timestamps. R36 and final review must assess these gaps; they are not claimed as covered.

That paragraph records the M2 acceptance boundary. The later [R36 measurement work](m7-measurement-validation.md) adds and validates the first two regressions.
Independent review accepted those two regressions and the coverage interpretation as a partial slice.
The parent subsequently passed real recovery tests with PostgreSQL process clocks offset by +24 and −24 hours.
Independent review accepted the clock-specific work and whole-task R36 with no findings.
The earlier M2 acceptance record is unchanged. The reviewed clock model is process interposition, not a host-clock or NTP adjustment test.

### Validation actually run

| Command | Result |
| --- | --- |
| New dispatcher regressions before the fix | Failed for cancellation loss, malformed-success retry, and invalid settings; saved as `out/remediation/m2-dispatch-red.log`. |
| `go test -count=1 ./schema ./runtime` | Passed after review fixes. |
| `go test -race -count=3 ./schema ./runtime` | Passed after review fixes. |
| `go test -race -count=1 ./schema ./runtime` | Passed again after explicit default/backoff tests. |
| `go test -p 1 -tags=integration ./schema ./runtime ./cmd/effectusd -count=1` | Passed against a new isolated PostgreSQL 16 fixture. |
| `go test -count=1 ./...` | Passed before the second-review fixes; focused, race, and PostgreSQL suites were rerun afterward. Full final validation remains R38. |
| `just guardrails` | Passed with three intentional additive API entries. No budgets were raised. |
| Primary LSP checks | No errors in checked M2 files. An auxiliary opengrep scan later reported incomplete coverage; that is not evidence of a clean auxiliary scan. |
| `git diff --check` | Passed. |

An initial PostgreSQL regression fixture accidentally admitted an execution with no selected plans, which correctly became terminal before leasing. The fixture was corrected to include a real plan, saga, and step; no production check was relaxed. The first guardrail run correctly rejected the three new public declarations. The inventory was then edited for only the optional renewal interface and its two implementations, after explicit compatibility review.

Logs are under `out/remediation/`, including `m2-focused-final.log`, `m2-race-final.log`, `m2-postgres-final.log`, `m2-options-final.log`, and `m2-guardrails-final.log`.

The task-owned fixture is `effectus-remediation-c12270d5`, bound to loopback port `32791`, with label `effectus.remediation=388c3cb-direct`. It uses the repository's pinned PostgreSQL image. Its connection record is a mode-0600 ignored file, `out/remediation/postgres-fixture.json`; credentials are not copied into documentation. No pre-existing database was used or destroyed. Remove only this task-owned container after remaining validation.

A destination may still commit after cancellation if it ignores cancellation. A failed completion write cannot establish whether that effect committed. Sink-side idempotency and fencing remain required; this work does not claim exactly-once side effects.

## M3: R09-R13 identity, replay, and ownership

### Implemented contracts

- **R09:** `TerminalExecutionError` preserves failed and every blocked state for terminal callers, on both first execution and replay. `errors.As` exposes the state; `errors.Is` recognizes `ErrTerminalExecution` and the retained dependency sentinel. Accepted-only replay still returns the durable identity without a business error. Tests drive real permanent failure, unknown outcome, stale fence, missing historical dependency, and failed compensation, and test every terminal state without another executor call.
- **R10:** New admission labels and an expected digest must match the active immutable generation before artifact or execution writes. Replay checks its persisted labels and pinned artifact, not the currently active version. Startup replacement can therefore replay an older version without relabeling it. Invalid labels produce no artifact write, record, or invocation.
- **R11:** Admission normalization uses an owned copy. Empty merge policy becomes `merge` before hashing; unsupported policies fail. Declared integers, bytes, lists, and other values are normalized before identity comparison. The identity view omits undeclared intermediate object containers, retains their flattened leaves, and retains declared objects as values. Explicit dotted facts win collisions as they do during execution. Unknown leaf values remain significant. The legacy `admissionHash` helper and goldens are unchanged. If a stored hash differs, the engine proves equivalence against normalized frozen facts under the pinned environment. It does not rewrite old hashes. Tests cover old hashes, omitted/explicit merge, numeric spellings, bytes/base64, typed lists, nested/dotted identity, true conflict, and caller ownership.
- **R12:** Execution records are read from the ledger on every call, not served from a stale cache. Same-engine calls for an execution are coalesced. CAS conflicts refresh authoritative records and retry at most three times; newer terminal states are adopted, never overwritten. Both stores reject direct state writes to terminal or recovery-owned records without modifying plan rows. A normal caller cannot impersonate an owner by copying a lease from the record. Recovery requests must be terminal resumes of the same execution. Built-in stores perform a non-shrinking minimum-duration authority probe before invocation.
- **R13:** Only active execution gates and active historical-generation resolutions are retained. Idle retention is zero. Historical resolution is single-flight by digest while callers overlap. Returned generations transfer ownership to the engine and close after their last user. Incorrect or failed resolver results close exactly once. `Engine.Close` stops new calls, waits for active calls, and then closes the active generation. Repeated calls return the first resource-close error. Only that first error is retained, so repeated historical close failures do not build an unbounded error chain. Borrowed stores and fencing providers are not closed by the engine. Do not call `Engine.Close` from one of its executing callbacks.

New runtime files separate identity, state, and lifetime handling: `runtime/engine_identity.go`, `runtime/engine_state.go`, and `runtime/engine_lifetime.go`. Corresponding regression files cover their public behavior. No protobuf or SQL schema changed. The six additive public inventory entries are `TerminalExecutionError`, its three error methods, `ErrTerminalExecution`, and `ErrExecutionBusy`. No inventory budget changed.

### Review fixes and fixture corrections

The first independent review required validation of recovery request shapes before execution, correct busy-state classification after the final CAS refresh, bounded close-error retention, and specific missing regressions. Those fixes and tests were added. The second read-only review **accepted R09-R13 with no remaining correctness or ownership blocker**.

The existing 64-caller HTTP race fixture stopped at its admission barrier because a single engine now coalesces those calls before admission. A bounded Go timeout identified the barrier, not a product shutdown deadlock. The test now uses 64 independent engines sharing the same ledger and outbox. All requests still synchronize at `AdmitExecution`, and all original status/conflict assertions remain. Independent review confirmed that this change does not weaken the fixture. An outer timeout left a test process with PID 2219308. The M4 check confirmed that this PID was absent. That check did not issue a kill command. Subsequent commands use Go's own `-timeout` for diagnostic stacks and test-process termination.

A new in-memory CAS fixture initially used a noncanonical saga ID. It was corrected to use `StableSagaID`; production admission validation was not relaxed. M2 heartbeat mocks now distinguish the engine's minimum-duration authority probe from periodic lease extension, preserving the in-flight renewal-loss and completion-race assertions.

### Validation actually run

| Command | Result |
| --- | --- |
| `go test -count=1 ./runtime ./schema` | Passed with the new first-attempt, replay, identity, ownership, and CAS regressions. |
| `go test -race -count=3 ./runtime ./schema` | Passed before the final review additions. |
| `go test -timeout=60s -count=1 ./...` | Passed after review fixes and the HTTP fixture correction. |
| `go test -race -timeout=90s -count=1 ./...` | Passed after all M3 changes. |
| `go test -race -p 1 -tags=integration -timeout=90s -count=1 ./schema ./runtime ./cmd/effectusd` | Passed on the isolated PostgreSQL fixture. Terminal/owner guards preserve execution and plan rows. |
| `go vet ./runtime ./schema ./schema/ledger ./cmd/effectusd` | Passed. |
| `just guardrails` | Passed after explicit review of the six additive declarations. |
| Primary LSP checks | No errors in the checked M3 implementation and test files. |

Logs include `out/remediation/m3-full.log`, `m3-race-final.log`, `m3-postgres-race.log`, `m3-guardrails-final.log`, and `m3-vet.log`. The historical failed HTTP-barrier run is `m3-daemon-timeout.log`.

R14-R40 remain open. In particular, transports must map and sanitize the new error types, daemon shutdown must honor the engine's draining ownership contract, and final documentation/measurement gates must run after those changes. Passing current suites does not complete those tasks.

## B02: Retried worker timeout; direct continuation authorized

Workflow `30b501d3-13ab-40e8-bfae-3a405e464c88` failed when `language-implement` child `85294634-dc1e-4cb7-9445-afcde1ecf9e3` reached its 30-minute timeout. The parent verified terminal/stopped status and zero active writers before modifying source. The main checkout remained at `388c3cb23943fc9032bcf37a18277688596972d6`, with unstaged partial work.

Recovery copies are `out/remediation/language-timeout.patch` and `language-timeout-untracked.tar.gz`. The patch excludes the pre-existing user change to `.github/scripts/release-preflight.sh`. Focused and race tests passed on the recovered changes; independent M1 review then accepted them. The user had already authorized direct work if background execution failed, so no unapproved runner fallback occurred.

The release script hash was rechecked after M2 and remains `3ee2921f9f14c5f32ad73512cf2ea718ea897ffb9037c963cc214b5386a7599b`. No changes are staged; no commits, resets, pushes, releases, or deployments occurred.

## B01: Background worker cannot start (historical; retry succeeded)

The workflow failed on 2026-09-07 before its first child session started.

- Workflow: `580c08bf-bbc1-40b3-bf5b-37880e85adcb`
- Mission: `f46c3c4f-e8fd-4788-8656-4d23e206b959`
- First step: `language-implement`, assigned R01-R04
- Attempted child run: `8fec2c0a-0409-4b1d-a183-c2aeea618038`
- Workflow state: `failed`
- Child session: unavailable. No session file was persisted, so the child cannot be resumed.
- Active async capacity after inspection: zero active runs.

The runner reported:

```text
Failed to start async run '8fec2c0a-0409-4b1d-a183-c2aeea618038': Background children require pi installed as the npm package (@earendil-works/pi-coding-agent) with its dependencies; /home/josephcox/.nvm/versions/node/v26.1.0/lib/node_modules/@earendil-works/pi-coding-agent does not provide @earendil-works/pi-server, @earendil-works/pi-server/unix, @earendil-works/pi-client/unix, so the async runner cannot create child sessions. A standalone pi binary cannot run background children.
```

This is a runner installation/module-resolution failure, not an Effectus test failure.
The parent has not independently established why those modules cannot be resolved.
No implementation, tests, independent reviews, or integration checks ran in this workflow.

The preflight also reported lane-key mismatches because milestone labels differed from actual child keys.
Those advisories did not cause the startup failure. Correct the declarations before any same-protocol relaunch.

### Preserved diagnostics

These temporary files may help diagnosis but are not the durable task record:

- Workflow script: `/tmp/effectus-remediation-workflow.js`
- Run directory: `/tmp/pi-subagents-uid-1000/async-subagent-runs/580c08bf-bbc1-40b3-bf5b-37880e85adcb`
- Events: the run directory's `events.jsonl`

The checklist and this report preserve the scope and blocker independently of those temporary files.

## Repository verification after failure

The parent checked the actual checkout after the failure:

- Repository/cwd: `/home/josephcox/dev/effectus`
- Branch: `main`
- HEAD: `388c3cb23943fc9032bcf37a18277688596972d6`
- Isolation: shared main checkout, with one planned writer. No isolated lane worktree was created.
- Tracked changes: only the pre-existing `.github/scripts/release-preflight.sh` edit, 8 insertions and 8 deletions.
- New task files: `docs/REMEDIATION.md` and documents under `docs/audits/`.
- Staged changes: none.
- Implementation source changes from the failed workflow: none.

The preserved release script still has SHA256:

```text
3ee2921f9f14c5f32ad73512cf2ea718ea897ffb9037c963cc214b5386a7599b
```

Commands used for this check: `git branch --show-current`, `git rev-parse HEAD`, `git status --short`, `git diff --stat`, and `git diff --cached --stat`.
The parent also hashed the release script and confirmed that no worker validation report existed before this blocker report.

## Original recovery options (approval subsequently received)

The parent stopped the failed execution path. It did not change the Pi installation or switch to another execution mode.

Available recovery paths:

1. Authorize direct work in the parent session, starting at R01 and following the same written acceptance criteria.
   Independent review remains a required task, not an assumed substitute for self-review.
2. Authorize diagnosis and repair of the Pi background runtime, then relaunch the same subagent protocol from R01.
   The failed child cannot be resumed because it has no session file.

The user has now authorized a retry after their Pi upgrade/restart, with direct-session work if it still fails.
The parent rechecked the repository state and preserved user changes before the retry.
No implementation or integration task is complete merely because the audit and checklist were written.
