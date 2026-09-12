# R34–R35 documentation contracts and site validation

## Status

R28–R35 and combined M6 passed independent acceptance.
The separate R29–R34 receipts retain their original scope and validation boundaries.
No production handler, wire identity, migration, Go dependency, public inventory, or surface budget changed in this slice.

## R34 acceptance map

| Requirement | Executed contract |
| --- | --- |
| Per-command help | Compiler tests read each documented command/required-flag row, execute actual help, and compare its flags. Daemon tests execute both help spellings and compare registered flags. |
| Defaults and required combinations | `TestDocumentedDaemonFlagNamesAndDefaults` compares the table with registered defaults. Compiler tests omit each documented required flag and then execute a valid command. Existing daemon mode tests and the documented startup subprocess gate cover required combinations. |
| Recipes | Contributor/agent recipe references resolve against the real Justfile. The accepted R31 recorder tests execute migration-before-tests and failure ordering; its owned-PostgreSQL evidence remains separate. |
| Snippets | Accepted R30 tests execute the tutorial commands and check exact `.eff`/`.effx` snippets, bindings, outcomes, and compiler round trips. |
| JSON and transport contracts | Accepted R32 tests load exact HTTP JSON from documentation and exercise loopback HTTP/raw TCP. Accepted R33 tests run actual Go/Python clients with authentication, replay, and separate TLS cases. |
| Local links | The actual MkDocs renderer builds copied documentation with the actual configuration. Negative cases reject missing documents/anchors, omitted navigation pages, unrecognized relative links, and absolute links. |
| Source indexes | Repository-only indexes also get inline local-file target checks, with missing, encoded, absolute, and escaping target fixtures. These checks do not implement a general Markdown parser or validate source-index fragments. |
| Stale claims | Targeted negative phrases supplement existing CLI/executor/compatibility fixtures and source review. Historical release notes must direct readers to the current command reference. These guards do not prove every sentence correct. |

`cmd/effectusc/docs_contract_test.go` now executes actual compiler subprocesses from unrelated working directories.
It preserves the existing negative fixtures and uses valid temporary bundles and output files.
Each required flag must fail with its specific missing-flag error when omitted.

`cmd/effectusd/docs_contract_test.go` no longer maintains a second partial flag list.
Its actual help processes have bounded lifetimes and cleared database/token environment values.
The separate default-table regression remains unchanged.

`internal/guardrails/docs_site_test.go` copies the real site into temporary directories.
It never inserts a negative link into the checkout or weakens the real configuration.
The positive site must build; each negative case must produce the expected warning and strict failure.
The copy helper rejects symlinks and other nonregular entries.
A separate subprocess test proves missing MkDocs causes an explicit skip in optional mode and failure in required mode.

The required gate is:

```bash
EFFECTUS_REQUIRE_DOCS=1 go test -count=1 -timeout=90s ./internal/guardrails -run 'DocumentedStrictSite|DocumentationCopy'
```

CI now installs the pinned documentation requirements and runs the uncached documentation contracts with `EFFECTUS_REQUIRE_DOCS=1`.
The existing Pages workflow still runs the strict site build. This records configuration and local execution, not a new remote CI run or deployment.

## R35 corrections

- The source and site indexes now link the HTTP/Go API references, gRPC capability matrix, and Buf compatibility guide.
- The navigation includes the current references and validation reports rather than leaving them as omitted pages.
- Indexes distinguish inbound transports from outbound executors, startup admission from pinned historical replay, and embedded memory from PostgreSQL durability.
- The glossary removes unsupported standard-function-library, arbitrary flow-branching, automatic reversal, and dynamic plugin claims.
- Current guidance requires destination deduplication coordinated with business commit; fencing is a separate requirement, not an alternative.
- The v0.4 release-line notes list actual durable-demo prerequisites. Older notes are explicitly historical and retain their original version-specific content.
- Secret rotation now describes one token per process, a coordinated client change, and a controlled outage—not an unsupported overlap period.
- The saga guide distinguishes atomic ledger/outbox admission from later guarded operations and requires the complete migration set.
- The dependency guide matches CI's moderate-severity npm audit including development dependencies. No new vulnerability-scan result is claimed.

The stronger MkDocs validation settings come from the pinned [MkDocs 1.6.1 configuration reference](https://github.com/mkdocs/mkdocs/blob/1.6.1/docs/user-guide/configuration.md#validation).
Omitted pages, absolute/unrecognized links, and missing anchors now produce warnings that fail the existing strict build.
The existing repository-only `docs/README.md` exclusion remains; its local file targets are covered separately.
Remote URLs are not fetched by these gates.

## Executed validation

All logs are under `out/remediation`.

- `m6-r34-behavioral-initial.*`: compiler table/help/required-flag execution and strict renderer negative fixtures passed under the race detector.
- `m6-r34-source-contracts-initial.*`: daemon help passed; source-index checks found AGENTS.md had no inline links. The guide now links the real contributor instructions and explains the required renderer gate. The nonempty-link assertion was retained.
- `m6-r35-site-initial.*`: the original strict build passed but reported 13 omitted pages at INFO severity.
- `m6-r35-site-stricter-red.*`: stronger validation correctly failed on those omissions.
- `m6-r35-site-corrected.*`: navigation corrections passed without warnings.
- `m6-r34-r35-results.json`: repeated focused race tests passed; the initial full run found the saved Go reconstruction artifact described below.
- `m6-r34-r35-final-results.json`: unchanged full repository race command, guardrails, strict site build, vet, and actionlint all passed after the artifact relocation.

The final full run selected both `EFFECTUS_REQUIRE_DOCS=1` and the actual R33 Python venv interpreter.
It did not count unavailable tools or an unselected Python gate as validation.
Primary diagnostics passed for all 18 scoped code/configuration/documentation files before the final checks.
Session-cache diagnostics later reported no error issues across 189 diagnosed files; that is not a fresh project-wide analyzer scan.

## Review-artifact relocation

The first full run found the incomplete R33 source reconstruction as a Go package under `out/remediation`.
That package intentionally contained only the three files needed for the prior formatting proof, not a runnable client.
The product tests were not changed or filtered to hide it.

The parent verified all three original SHA256s against the original R32–R33 manifest, created and verified an exact archive, and moved the directory to:

`out/remediation/.m6-r32-r33-frozen-reconstruction`

The hidden directory retains all original bytes. The archive preserves the former paths.
Location mapping and checks are in `m6-r32-r33-frozen-reconstruction-location.json`.
The original manifests and patches were not overwritten. The failed full-run log remains available.
The same `go test -race -count=1 -timeout=90s ./...` command then passed.

## Independent review and corrections

The reviewer accepted R29 and R34 but required two documentation corrections before R28, R35, and combined M6 acceptance.
The original run `edc134b3-1c5e-4c80-a80d-1f5eeee28b4a` ended with `Request was aborted` after delivering its report.
The same reviewer confirmed that source review in completed receipt run `4be9d52e-2a57-4b09-b812-710fa4caaf9d`.
The receipt requires both findings to be corrected and reviewed. It clarifies the original theory-index finding from P2 to P1.

The delivered report and receipt are preserved as `m6-final-review-delivered.md` and `m6-final-review-receipt.md` under `out/remediation`.
The recovery did not repeat broad source investigation or execute tests.
The reviewer distinguished its source assessment from parent-executed tests, hashes, and archive checks.
The abort and same-protocol recovery do not imply any new validation.

The parent verified all 35 amended scope and 56 reference hashes before recording acceptance or applying corrections.
The four-line `docs_source_test.go` alignment amendment remains explicit in `m6-final-review-manifest-format-update.json`.
Repeated formatter notifications caused no further byte drift, as recorded in `m6-final-review-receipt-snapshot-check.json`.
Original manifests, archives, patches, and reports remain unchanged. This acceptance record is a later audit amendment.

The correction changes five normative or index pages, not production dispatch or embedded behavior:

- Lifecycle, coherent-flow, guarantee, and saga guides now distinguish default blocking from checked, attempt-bounded `SINK_GUARANTEED` retries with stable identity.
- They state that `KEY_REQUIRED` alone does not authorize unknown-outcome retry and that destination deduplication must accompany the business commit.
- The theory index labels legacy list/continuation APIs historical and unsupported as a current embedded path.

`TestDocumentedCheckedUnknownOutcomePolicy` compiles and round-trips a real source bundle before executing its checked plan.
Its four cases cover sink-guaranteed unknown-to-success, key-required blocking, exhausted retries, and the default one-attempt budget.
It checks terminal and persisted states, attempt history, stable metadata, and replay without another invocation.
A process-local destination fixture shares one recorded commit across repeated keys. This does not establish a deployed destination guarantee.
The existing source-document guards also reject the five original false claims. No assertion was removed.

`m6-unknown-policy-initial.*` preserves an initial test-only mismatch between a typed ledger state and the string execution-result state.
The test now compares the expected state string at that boundary and retains typed ledger/error comparisons.
No production change was needed.
`m6-unknown-policy-results.json` records five repeated focused race runs and three repeated unknown/retry/recovery/disposition race runs, all passing.
The final fixture also counts business commits under the same lock as its deduplication record.
`m6-policy-fix-results.json` records validation after that assertion and the new source-document guards:

- Three repeated race runs of documented contracts across runtime, compiler, daemon, and guardrails passed.
- The unchanged full-root `go test -race -count=1 -timeout=90s ./...` command passed.
- `just docs`, `just guardrails`, and `go vet ./...` passed.

These runs required MkDocs and selected the same absolute R33 Python venv interpreter.
Primary diagnostics passed for all 11 correction and audit files before those commands.
The full-root run precedes this results paragraph. A final documentation gate covers the audit amendment before the correction snapshot.
At the correction snapshot, R28 and R35 remained unchecked pending re-review.

## Combined M6 acceptance

Fresh reviewer run `166be6bc-cae8-4ee7-8b27-80ccf6730028` accepted R28, R35, and combined M6 with no findings.
The complete delivered report is preserved as `out/remediation/m6-policy-accepted.md`.
It independently assessed the 11-file correction patch, current sources, frozen references, and parent validation records.
The reviewer ran no commands or tests and made no source changes.

The accepted snapshot is `m6-policy-review-manifest.json`, covering 11 correction files and 86 references.
Its 27,443-byte patch has SHA256 `c45de5d5e79a76c19dd100f36d4ea3730f118ed985884872baf14372b91b7ee8`.
Its 97-member archive has SHA256 `89417873959a31d2eeb7b738ab293b56560299e1819e0ccb66c35e68456167ae`.
Before recording acceptance, the parent verified all 97 source hashes, 21 evidence hashes, archive members, and both reported artifact hashes.
The parent also verified unchanged HEAD/main, an empty index, and the protected release-script hash.
These checks are recorded in `m6-policy-acceptance-verification.json`.

This acceptance section and the checklist/status updates are later audit amendments, not changes to the preserved review snapshot.
R36–R40 remain open. M6 acceptance does not establish production readiness or a new external-service gate.

## Boundaries

Later R34–R35 contributor/index amendments do not rewrite the historical R30–R33 or M5 acceptance snapshots.
No test or source change to the accepted R33 clients was needed.
The isolated PostgreSQL fixture remains available, but these gates do not add a new database integration result.
Kafka deployment, representative coverage/benchmarks, final correctness/usability review, and R36–R40 remain open.
