# Aggregate review and finding reconciliation

## Status and scope

**R39 and R40 independently accepted for local remediation scope.**
R01–R38 have independent milestone acceptance. Those receipts do not establish correct composition of all changes.
The first aggregate review accepted the boundary/API and usability lanes. Its core lane required three P1 corrections.
The parent reproduced all three findings, applied corrections, and passed focused and broader checks below.
The first correction review accepted C1, C3, and C2's resume path, but found a remaining admission-replay cancellation defect.
The parent reproduced and corrected that follow-up. Independent re-review accepted the final diff and reconciliation with no findings.

The original audit baseline is `388c3cb23943fc9032bcf37a18277688596972d6`.
The aggregate diff includes checkpoint `673d6c9d87c5aa69ce88306ca424c6788d9e3de2` and later local changes.
It is not a diff against the checkpoint alone.
All later work remains local and unstaged. No additional commit, push, deployment, publication, or fixture removal occurred.

The protected user file is `.github/scripts/release-preflight.sh`.
Its SHA256 remains `3ee2921f9f14c5f32ad73512cf2ea718ea897ffb9037c963cc214b5386a7599b`.
Every remediation patch excludes it. The original freeze includes it only as a read-only reference.

## Original aggregate review

Workflow `6ba36f55-9339-4919-b136-029a2fb15768` ran three independent read-only lanes.
The reviewers inspected source, patches, tests, documentation, and selected saved evidence.
They did not execute tests, calculate hashes or measurements, edit files, or access services.
All execution and numerical verification below belongs to the parent.

| Lane | Child run | Original verdict | Preserved report under `out/remediation/` |
| --- | --- | --- | --- |
| Core | `c8869d6c-4543-4fcc-b2b7-844cae429795` | Required fixes: three P1 findings | `r39-core-first.md` |
| Boundaries/API | `9f161318-d491-4d7d-a90a-5532b384f284` | Accept, lane only | `r39-boundaries-accepted.md` |
| Usability | `85f2157a-ce13-49cd-b39c-160badbd9061` | Accept, lane only | `r39-usability-accepted.md` |

The reviewed manifest is `out/remediation/r39-review-manifest.json`.
Its SHA256 is `9fd40f8e9a41d2d9647ff374273c9593ce9bef0b8059399b1381457896dc54e2`.
It contains 171 changed files, 198 references, and 155 evidence files.
The parent verified all 369 source/archive members and all 155 evidence files before corrections.
`r39-initial-review-verification.json` records this check and byte-identical report preservation.
The original archive, patches, manifest, navigation supplements, and reports remain intact.

Reviewer search routing lacked the requested index tools. The parent supplied bounded source reads, patch indexes, and numbered excerpts.
Those supplements changed navigation, not source scope or reviewer permissions.
All three lanes completed before the parent changed source.
A later read-only helper lookup stopped after repeated tool calls. It supplied no usable result and made no source changes.
The parent continued its already-authorized implementation work on the identified files, without launching a replacement execution mode.

## C1: Empty bytes and durable step identity

`runtime/checked_workflow.go` resolved a bytes literal to canonical base64.
`schema/saga_checked.go` instead copied raw bytes. An empty copy became `nil`, which JSON encoded as `null`.
PostgreSQL atomic admission stored the first representation. Re-enqueue rejected the second representation because its argument hash differed.
The in-memory execution ledger did not consume `DurableAdmission.InitialSteps`, which hid this mismatch in earlier execution tests.

Both resolvers now use padded base64, including `""` for empty bytes.
No hash function, persisted hash, stable identifier, checked artifact, or conflict check changed.
The tests cover nil bytes, empty bytes, nonempty bytes, a nested list, and a nested object.
The unit contract explicitly seeds initial outbox intent rather than claiming atomic behavior from the in-memory execution ledger.
The PostgreSQL contract verifies accepted-only admission, stored initial intent, a new engine, terminal resume, and replay without another invocation.

`TestCheckedBytesLiteralKeepsExistingNonemptyIdentityAndRejectsConflict` also preserves an older raw-byte intent's canonical identity.
Historical admission hash goldens still pass. This is not an automatic repair or migration of noncanonical stored intent.
Strict identity conflicts remain errors, rather than permission to rewrite old hashes.

Tests: `runtime/bytes_literal_identity_test.go` and `runtime/bytes_literal_identity_integration_test.go`.

## C2: Historical resolution cancellation

A canceled single-flight waiter previously became a permanent missing dependency.
The engine then persisted `blocked_dependency` through a cancellation-independent context.
A canceled resolution leader could propagate the same permanent failure to healthy waiters.

After the admission-replay follow-up below, both replay entry paths retain wrapped artifact lookup cancellation and deadline errors.
Historical resolution also retains those errors.
Neither error establishes permanent dependency failure, even when another request caused the shared resolver to stop.
A healthy waiter can retry after a leader cancels. The change does not promise automatic retry within that same call.
An early canceled load releases a supplied recovery lease nonterminally through the existing owner/token/revision/unexpired-lease CAS.
The engine never adopts a lease token from a record as write authority.
Genuine unavailable or mismatched historical dependencies retain their existing failure behavior.

`runtime/historical_cancellation_test.go` gates two historical executions before cancellation.
It covers follower cancellation, leader cancellation, unchanged records, subsequent completion, generation closure, and empty active-entry maps.
It also covers recovery cancellation, cleared lease fields, later recovery, and wrapped artifact lookup context errors.
Every test cancels and joins its users before engine cleanup, including failure paths.

### Admission-replay follow-up

Reviewer `9122f0d5-ae63-4c91-8614-9069cb22a720` found that `matchReplay` still discarded lookup cancellation through `%v` formatting.
Terminal-wait admission replay could therefore persist a permanent block.
Accepted-only admission replay did not enter that terminalization branch, but still lost the cancellation classification.
The preserved report is `out/remediation/r39-corrections-first-review.md`.
Its source assessment accepted C1, C3, and resume-path C2, but required fixes for overall R39 and R40.

`runtime/engine_identity.go` now returns wrapped cancellation and deadline errors before missing-dependency classification.
Other lookup errors and artifact identity validation keep their existing behavior.
The lookup regression now covers resume, matching terminal-wait admission, and matching accepted-only admission for both context errors.
It requires unchanged durable state and revision, no terminal error or invocation, and later completion of the original identity by a healthy engine.
The four new admission cases failed before the fix. All six cases passed afterward, including the two existing resume cases.
This follow-up changes no public interface, hash, state, authentication check, or recovery-lease rule.

## C3: Immutable invalid results

Result normalization could reject a result after the outbox had persisted success.
The execution stayed nonterminal because the unfinished saga still had state `running`.
Each recovery attempt read and rejected the same immutable result again.

An unusable persisted result now produces an execution-level `blocked_dependency` disposition.
The engine records it through the existing durable state or recovery-lease CAS.
It does not change successful dispatch, step, result, or attempt evidence.
It does not mark the unfinished saga complete, fabricate a business failure, or start compensation.
The execution is terminal and no longer eligible for engine recovery. The saga remains unfinished as evidence of withheld downstream work.
This distinction uses existing states and interfaces. No public API or protobuf identity changed.

The extended `TestLanguageConformanceInvalidResultsDoNotReachNextStep` verifies typed terminal failure and replay, accepted-only replay, and zero recovery work.
It also verifies the retained saga/dispatch disposition, one successful attempt, and no downstream invocation.
`runtime/result_contract_disposition_test.go` injects failure of the blocking-state write.
That failure does not claim terminal success and preserves the store error in the error chain.
A new engine then recovers the frozen result once, records the block, clears the lease, and never invokes the effect again.

A fresh diagnostic also identified the unused private `isUnconditionalExtensionPlan` helper.
Exact source search found only its declaration. The parent removed it without changing a public declaration.

## Parent validation

All commands below ran on Go 1.25.13, Linux arm64, unless a tool selects its own runtime.
Full Go runs selected required documentation and Buf checks, plus the absolute pinned-requirements Python example interpreter.
The runner verified the original owned PostgreSQL container, image, labels, running state, and loopback binding before database tests.
Credentials remained in mode-0600 local state and were not included in review inputs.

| Gate | Result | Receipt under `out/remediation/` |
| --- | --- | --- |
| Bytes unit reproduction | Four empty/nested cases failed with identity conflict | `r39-bytes-unit-red.json` |
| PostgreSQL bytes reproduction | Four empty/nested cases failed before invocation | `r39-bytes-postgres-red.json` |
| Bytes and historical-hash races | Five repetitions passed | `r39-bytes-unit-green.json` |
| PostgreSQL bytes races | Three repetitions of all five cases passed | `r39-bytes-postgres-green.json` |
| Historical cancellation reproduction | Follower, leader, recovery, and lookup cases failed | `r39-historical-cancellation-red.json` |
| Cancellation and related ownership/recovery races | Five repetitions passed | `r39-historical-cancellation-green.json` |
| Invalid-result reproduction | Missing terminal disposition and missing write-failure path reproduced | `r39-invalid-results-red.json` |
| Result, terminal, and unknown-policy races | Five repetitions passed | `r39-invalid-results-green.json` |
| Full-root race suite | Passed, zero test skips, 13 packages with no test files | `r39-root-race.json` |
| Sequential PostgreSQL integration race suite | Passed, zero test skips, one package with no test files | `r39-postgres-integration-corrected-race.json` |
| Full `go vet ./...` | Passed | `r39-vet.json` |
| Repository guardrails | Passed, no budget or inventory update | `r39-guardrails.json` |
| `just lint` | Passed | `r39-lint.json` |

The full-root command was:

```bash
go test -race -count=1 -timeout=90s -json ./...
```

The database command was:

```bash
go test -race -count=1 -p 1 -tags=integration -timeout=90s -json \
  ./runtime/... ./schema ./cmd/effectusd
```

These first-pass runs followed C1, C3, and the resume-path C2 correction. They preceded this report and the admission-replay follow-up.
The initial required documentation race, strict site, STE, and diff checks then passed on the first correction report.
Their receipts are `r39-documentation-race.json`, `r39-site.json`, `r39-ste.json`, and `r39-diff.json`.
The package-skip counts describe `[no test files]`, not skipped selected tests.

### Admission-replay follow-up validation

| Gate | Result | Receipt under `out/remediation/` |
| --- | --- | --- |
| Matching admission lookup reproduction | Both context errors failed in terminal and accepted-only modes. Resume cases passed | `r39-admission-replay-cancellation-red.json` |
| Identity, cancellation, recovery, HTTP and gRPC races | Five repetitions passed, zero test skips | `r39-admission-replay-cancellation-green.json` |
| Full-root race suite after follow-up | Passed, zero test skips, 13 packages with no test files | `r39-replay-followup-root-race.json` |
| Full vet | Passed | `r39-replay-followup-vet.json` |
| Full lint | Passed | `r39-replay-followup-lint.json` |
| Repository guardrails | Passed | `r39-replay-followup-guardrails.json` |

The follow-up Go runs used Go 1.25.13 and followed the `matchReplay` correction and expanded regression.
The full-root run again selected required docs/Buf checks and the absolute Python example interpreter.
These runs preceded the follow-up prose edits. Subsequent documentation checks remain separate.
PostgreSQL integration, coverage, benchmark, model, and broker results keep their earlier snapshot chronology.

### Preserved test-harness corrections

The first PostgreSQL bytes test compared JSONB readback bytes with compact JSON.
PostgreSQL added whitespace, so that assertion failed before the target regression.
The original test and red remain as `r39-bytes-postgres-initial-test.go.txt` and `r39-bytes-postgres-initial-red.*`.
The corrected test re-canonicalizes readback, then checks exact canonical JSON and both expected and stored hashes.
No identity assertion or production check was removed.

The first broader integration command set `POSTGRES_DSN` but not the schema tests' `DB_DSN` alias.
It exited zero but skipped 13 schema integration tests. It is not a complete PostgreSQL gate.
`r39-postgres-integration-race.*` preserves that incomplete run.
The corrected runner supplies the same verified DSN through all three supported test/daemon aliases.
The unchanged suite then passed with zero test skips.
Both runner versions and their per-run hashes remain available.

## Independent final acceptance

Reviewer `0fdc1fba-e782-4c8d-a632-f358be4f3a0f` accepted the remaining C2 fix, final R39 diff, and R40 reconciliation with no findings.
The preserved report is `out/remediation/r39-r40-accepted.md`.
Its verdicts are **R39: ACCEPT**, **R40: ACCEPT**, and **merge OK for local R39/R40 scope only**.
Prior C1/C3, resume-path C2, boundary/API, usability, and unchanged reconciliation assessments remain applicable.
The admission-replay P1 is resolved. The original rejection reports remain unchanged.

The accepted manifest is `out/remediation/r39-replay-followup-manifest.json`.
Its SHA256 is `cbe99a59ecb4d30d8c2d2f7e90c7dc1df1e3315df795fea613e57e6527107c6f`.
The parent verified five changed files, 369 unchanged references, all 374 archive members, and all 266 evidence files.
`r39-r40-acceptance-verification.json` records that verification, byte-identical report preservation, and unchanged Git/protected-script state.
The reviewer assessed source. Execution, hashing, archive checks, and numerical summaries remain parent evidence.

The accepted archive retains the pre-acceptance checklist.
The later amendments record acceptance in this report, the central validation report, and the checklist only.
Status-only checks remain separate from the reviewed source tests.
Approval does not authorize publication, deployment, fixture cleanup, or any unchecked external gate.

## R40 finding reconciliation

This table covers all 32 original findings, without replacing the historical audit.
Disposition labels distinguish milestone acceptance from the later aggregate review.
R39 and R40 are accepted for local scope. The external gates remain unchecked.

| Finding | Tasks | Code, contract, and executable evidence | Disposition |
| --- | --- | --- | --- |
| F01: unavailable functions | R01 | Compiler and generation reject unavailable calls, including unreachable branches. [M1 evidence](remediation-validation.md#m1-r01-r04-executable-language-contract) | Accepted rejection contract, not a function implementation |
| F02: collection/value mismatch | R02 | `ir/value.go`, language conformance, bytes admission/re-enqueue tests | Accepted, including C1/C3 and the final aggregate review |
| F03: canceled disposition persistence | R05 | `schema/saga_dispatcher.go`, bounded independent persistence and fault-injection tests | Accepted. C3 additionally preserves state-write errors |
| F04: recovery authority | R06 | Ledger renewal, recovery preflight, latency and clock-offset regressions | Accepted, including C2 recovery cancellation |
| F05: terminal replay semantics | R09, R14 | `TerminalExecutionError`, HTTP/gRPC dispositions, historical and invalid-result replay | Accepted, including C2 admission replay and C3 disposition |
| F06: shutdown and startup cleanup | R15 | Daemon and gRPC drain/join tests, partial-listener cleanup | Accepted cooperative shutdown contract |
| F07: HTTP error taxonomy | R16 | `http_errors.go`, shared sanitized storage/terminal tests | Accepted |
| F08: HTTP bounds and routing | R17 | Exact-route, method, bounded-body and parsed-object tests | Accepted |
| F09: generation labels | R10 | Prewrite identity validation and negative admission tests | Accepted |
| F10: admission hashing/defaults | R11 | Pinned normalization, immutable caller facts, historical hash goldens | Accepted. Identity/hash and admission-replay regressions passed again |
| F11: cached state and CAS | R12 | Durable refresh, bounded conflict handling, owned/terminal row tests | Accepted. PostgreSQL races passed again |
| F12: historical generation ownership | R13 | Coalesced resolution, entered-call drain, counted closure tests | Accepted, including C2 cancellation and closure |
| F13: outbound executor JSON | R18 | `invocation/http.go`, bounded single-value/EOF and unknown-outcome tests | Accepted |
| F14: inbound argument integrity | R19 | `executorhttp/handler.go`, canonical hash, conflict and forgery tests | Accepted integrity contract, not business deduplication |
| F15: gRPC capability truth | R14, R25 | Additive disposition fields, preserved wire identities, 3-service/19-RPC matrix | Accepted |
| F16: gRPC constructor behavior | R20 | Pre-bind validation, explicit authenticated modes, listener ownership tests | Accepted |
| F17: transport identity | R21 | Shared HTTP/gRPC admission identity and conflict tests | Accepted |
| F18: CLI behavior and output | R22, R23 | CLI subprocess contracts, explicit migration mode, atomic alias-safe output | Accepted |
| F19: Buf compatibility API | R26 | Confined files, owned configuration, bounded commands and concurrency tests | [Accepted](m5-api-validation.md) |
| F20: defaults and negative options | R03, R07, R24 | Immutable validation defaults and constructor/dispatcher/recovery option tests | Accepted |
| F21: supported Go API | R27 | `docs/go-api.md`, public inventory, compatibility imports and guardrails | Accepted. No R39 public-surface change |
| F22: lifecycle claims | R28 | Current lifecycle/architecture docs, checked unknown-retry policy test | [Accepted](m6-contract-site-validation.md#combined-m6-acceptance) |
| F23: fictional configuration | R29 | Actual daemon flags/environment and separate library-option contracts | [Accepted](m6-contract-site-validation.md) |
| F24: incremental tutorial | R30 | Both tutorial dialects, bindings, diagnostics, replay and local deduplication | [Accepted](m6-tutorial-contributor-validation.md) |
| F25: contributor commands | R31 | Actual Just recorder contracts, explicit migration and owned-PostgreSQL execution | [Accepted](m6-tutorial-contributor-validation.md) |
| F26: HTTP reference | R17, R32 | Executed examples, routing/body/conditional contracts and raw-wire auth regression | [Accepted](m6-http-reference-validation.md) |
| F27: authenticated clients | R33 | Actual Go/Python subprocesses, shared replay identity and separate TLS negatives | [Accepted](m6-grpc-client-validation.md). Root gate passed again |
| F28: lexical-only docs tests | R34 | CLI, daemon, client, contributor and required-renderer behavior tests | [Accepted](m6-contract-site-validation.md) |
| F29: stale documentation indexes | R35 | Strict site, repaired indexes and historical/current distinctions | [Accepted](m6-contract-site-validation.md#combined-m6-acceptance) |
| F30: coverage and missing regressions | R04, R08, R36 | Exact coverage union, claim-latency/lease-race/clock tests, added R39 regressions | [R36 accepted](m7-measurement-validation.md). Coverage remains historical |
| F31: retention and performance evidence | R13, R37 | Active-only ownership and 28 bounded benchmark cases with repeated samples | [R37 accepted](m7-benchmark-validation.md). No measured improvement or capacity claim |
| F32: final validation and review | R38–R40 | Local validation, original three-lane review, correction reds/greens and this reconciliation | R38–R40 accepted for local scope. External gates remain open |

## Residual limits and unexecuted gates

These limits remain in force after the local corrections:

- Direct source spelling of minimum int64 remains an approved parser limitation. Facts, direct IR, and equivalent arithmetic have passing coverage.
- Cancellation and engine closure require cooperative callbacks. Noncooperative callbacks can prevent a drain from finishing.
- Destination exactly-once requires deduplication coordinated with business commit. Stable metadata, fencing, and local fixtures do not establish it.
- The embedded tutorial's locked deduplication is process-local. It is not a deployed destination guarantee.
- R36 coverage and R37 benchmark samples remain measurements of their original Go 1.26.7 snapshots. R39 did not rerun or relabel them.
- Coverage blind spots remain. Benchmarks do not establish production throughput, latency percentiles, measured improvement, or capacity.
- R38 formal results concern unchanged finite models, not the implementation or a temporal liveness property.
- Go 1.25.13 execution does not establish literal minimum Go 1.25.0 or universal architecture support.
- Local tool versions and environments differ from pinned CI. R38's corrected Helm label describes its saved preflight, not each chart command.
- Fresh primary checks and selected linters do not establish universal analyzer cleanliness. Prior auxiliary and Dockerfile diagnostic limits remain recorded.

External gate status follows. Local R39/R40 acceptance alone cannot close these gates.
The user later authorized an isolated standalone Compose stack and a retention-safe harness.
Its [first-run/restart checks and evidence reconciliation passed independent acceptance](standalone-compose-validation.md#independent-acceptance).
The separately authorized [Kafka-to-business-commit fixture also passed independent acceptance](kafka-business-commit-validation.md#independent-acceptance).
The other three gates remain open.

- [x] Standalone Compose first-run/restart: independently accepted for the bounded, retention-safe local fixture scope.
- [x] Combined Kafka → daemon → PostgreSQL → destination business commit: independently accepted for the bounded local lost-response, deduplication, redelivery, and restart scope.
- [ ] Remote CI on the exact final source: requires authorization to publish the final source and trigger or observe that CI run.
- [ ] Deployed Kubernetes, registry and production recovery paths: require authorized environments, credentials, deployment and recovery procedures.
- [ ] Production capacity: requires representative workloads, resources, destinations and a separate measurement plan.

All five earlier PostgreSQL containers and the earlier Kafka container remain retained and unchanged.
The new standalone stack also remains retained, including its PostgreSQL volume and successful migration container.
Any cleanup requires explicit authorization and fresh exact-ownership checks.
No final-source coverage, benchmark, Kafka deployment, model rerun, or production-readiness claim follows from the R39 checks.
