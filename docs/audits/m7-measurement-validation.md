# M7 measurements and remaining validation

## Status

R01–R37 and combined M6 have independent acceptance.
R38–R40 remain open. R36 acceptance covers the recorded measurements and regressions, not final production readiness.
Independent review accepted the PostgreSQL clock-skew work at both offsets and combined whole-task R36.
The parent passed 28 [representative benchmark cases](m7-benchmark-validation.md), with five measured samples per case.
Independent review accepted R37 with no findings. See the [benchmark acceptance record](m7-benchmark-validation.md#independent-acceptance).

## Coverage environment and commands

The measured toolchain was `go version go1.26.7 linux/arm64`, with cgo enabled and `GOTOOLCHAIN=auto`.
The module declares Go 1.25.0 and toolchain Go 1.25.13. These measurements do not establish execution on that minimum toolchain.
They must not be compared with older measurements as if the toolchain and machine were identical.

The parent required MkDocs with `EFFECTUS_REQUIRE_DOCS=1` and explicitly selected the absolute R33 Python venv interpreter.
The initial package inventory contains 33 packages and 173 source/test input hashes.
Its files are `r36-unit-packages.json` and `r36-unit-inputs.json` under `out/remediation`.
That inventory precedes the two new regression files described below.

Initial cross-package coverage, before those tests:

```bash
go test -count=1 -timeout=90s -covermode=atomic -coverpkg=./... \
  -coverprofile=out/remediation/r36-unit.cover ./...
```

After the delayed-claim and renewal/finish tests, both commands passed.
These preserved coverage profiles precede the separate clock-skew test. They do not include that later test's execution counts.

```bash
go test -race -count=1 -timeout=90s -covermode=atomic -coverpkg=./... \
  -coverprofile=out/remediation/r36-unit-after-gaps.cover ./...
go test -race -count=1 -p 1 -tags=integration -timeout=90s \
  -covermode=atomic -coverpkg=./... \
  -coverprofile=out/remediation/r36-postgres.cover -v \
  ./runtime/... ./schema ./cmd/effectusd
```

The second command received both `DB_DSN` and `POSTGRES_DSN` from the task-owned PostgreSQL fixture.
The parent verified its container ID, name, ownership label, running state, and loopback port binding before execution.
The report does not contain credentials. No unrelated database or container was selected.
The verbose PostgreSQL run reported no skipped tests.
The unit command was non-verbose, so absence of skip markers is not a complete skip inventory.
Required documentation tooling and an explicitly selected but unusable Python interpreter fail their respective gates rather than silently skip.

## Statement coverage

| View | Covered / measured statements | Coverage |
| --- | --- | --- |
| Initial unit run, all instrumented files | 5,677 / 12,526 | 45.32% |
| Initial unit run, handwritten repository files only | 5,301 / 8,449 | 62.74% |
| Unit plus PostgreSQL union after new tests, all files | 6,177 / 12,526 | 49.31% |
| Unit plus PostgreSQL union, handwritten repository files only | 5,801 / 8,449 | 68.66% |

The raw view retains every instrumented file.
The secondary view excludes 15 generated Go files and one dependency file under `tools/vscode-extension/node_modules/flatted`.
Generated files must have the standard generated-code marker before their package declaration.
The exact exclusions are recorded in `r36-unit-postgres-coverage-summary.json`. No handwritten low-coverage package was removed.

The initial profile contains 193,148 rows but only 9,485 unique source blocks.
The analysis merges counts for identical file/start/end positions and rejects conflicting statement counts.
Each unique block contributes its statement count once. It is covered if any participating test binary reported a positive count.
The unit/PostgreSQL union uses the same rule and retains the original profiles.
`go tool cover -func` reports 49.3% for the merged profile, consistent with the unrounded raw statement ratio.
Do not average package-output percentages: with `-coverpkg=./...`, those lines use a cross-package denominator.

Selected handwritten-package coverage from the merged profile:

| Package | Coverage |
| --- | --- |
| `compiler` | 75.24% |
| `ir` | 77.15% |
| `runtime` | 74.77% |
| `schema` | 72.60% |
| `schema/fencing` | 64.60% |
| `invocation` | 58.87% |
| `executorhttp` | 74.78% |
| `cmd/effectusc` | 92.44% |
| `cmd/effectusd` | 64.96% |
| `internal/daemon/kafka` | 56.76% |

Coverage measures executed Go statements, not all branches, inputs, schedules, SQL statements, Python code, or deployment behavior.
The Go client functions are represented in the profile. This does not establish general coverage capture for arbitrary subprocesses.
The standalone executor and legacy `compat/v03/embedded` packages have zero measured statements covered in this profile.
`cmd/effectusd.openDaemon` also has zero measured coverage. Separate flag, constructor, transport, and service tests do not erase that startup-path gap.
Kafka unit coverage does not establish a real broker or deployed daemon gate.

## R36 regression gaps

### Delayed non-renewing claims

`runtime/recovery_claim_delay_test.go` wraps the ledger through its base interface and explicitly verifies that the wrapper has no renewal capability.
It delays the claim before the server grants a fresh lease.
This distinguishes the worker's original local safe window from an incorrect new window started after the claim response.
The test requires zero invocation, a local deadline error while the caller remains live, and an unchanged accepted execution state.
It then waits for actual expiry, reclaims through normal APIs, and completes exactly one invocation under the new authority.
No returned timestamp or persisted deadline is rewritten.

The first primary check caught an invalid function assignment to the `Observer` interface.
A test observer now implements its two required methods. No production code or test assertion changed to hide that error.
Ten repeated race runs passed in `r36-delayed-claim-race.log`.

### PostgreSQL renewal versus finish

`schema/execution_lease_concurrency_integration_test.go` runs 16 barrier-started renewal/finish races per test execution against PostgreSQL.
It joins both database users before assertions and fixture cleanup.
Finish must succeed with the original handle. Renewal must either preserve authority and deadline monotonicity or reject the now-terminal lease.
The test checks one revision increment, terminal state, cleared recovery fields, and rejection of a subsequent renewal.
It does not assert a particular scheduler ordering or force row-lock overlap.
Five repeated race runs passed, for 80 barrier-started trials, with no skipped tests.
The fixture cleanup targets only the test's unique execution, saga, and artifact identities.

### Independent database-clock skew

The parent built two task-owned PostgreSQL fixtures with process-visible wall clocks offset by +24 and −24 hours.
`SELECT clock_timestamp()` demonstrated both offsets against the caller's real clock before any migrations or admission.
A separate query verified that the original PostgreSQL fixture remained unshifted.
No returned deadline, persisted timestamp, host clock, kernel clock, or existing fixture configuration was changed.

The image pins the PostgreSQL 16 Alpine base by digest and libfaketime 0.9.13 by commit and archive SHA256.
It initializes a fresh database as the unprivileged `postgres` user with the real clock.
Only the final PostgreSQL process tree receives the preload and signed offset.
`FAKETIME_DONT_FAKE_MONOTONIC=1` configures real monotonic time. The test does not directly instrument PostgreSQL's monotonic clock.
Image IDs, architecture, ownership labels, loopback bindings, and SQL clock observations are recorded.
Builder package repositories remain an input, so bit-for-bit image reproducibility is not claimed.

`TestPostgresRecoveryWithIndependentDatabaseClock` uses a real compiled generation, PostgreSQL admission, outbox, ledger, and recovery worker.
A wrapper selects only the test's unique execution through real PostgreSQL claim APIs and counts successful renewals.
It does not replace deadlines or renewal results.
The executor remains live for four 400 ms lease windows.
The test requires repeated renewal, exclusion of a competing owner after the original window, unchanged owner/token, and a future database-relative deadline.
It then checks terminal completion, cleared recovery fields, stale renewal rejection, and replay without another invocation.
Failure cleanup cancels and joins the worker before engine and database closure.

One initial race run and five subsequent race repetitions passed at each offset, without skips.
Three negative probes matched their expected failures: wrong clock offset, missing DSN, and missing offset.
These checks precede migrations and admission. Both `integration` and `clockskew` tags are required.
Once selected, the test fails on a missing or invalid fixture rather than skips.
The new fixture instructions are in `tests/fixtures/postgres/CLOCK_SKEW.md`.

A later fixture-only probe found that the initial signal traps removed the temporary password file but still started the server after HUP, INT, or TERM.
The original script and red observations are preserved.
Explicit signal-exit handlers now prevent server start and still clean the password file.
The checked-in Python test covers all three signals, invalid offsets, and preservation of an existing database marker.
Its first primary check required explicit narrowing of the optional shell path. It now fails explicitly if no shell is available and uses a generated test password.
No production code or assertions were weakened.

The corrected image was rebuilt, and two new containers were created without replacing the earlier fixtures or their evidence.
Both containers' entrypoint hashes matched the current source.
Five further race repetitions passed at each offset against that corrected image, with no skips.
The Python fixture tests also passed.
The full-root and tagged PostgreSQL race suites had passed immediately before this fixture-only correction, with Go JSON event capture.
They reported no test-level skips. Package-level skip events denoted packages with no test files.
The corrected-image runs and Python tests cover the subsequent fixture change. The preserved coverage profiles were not regenerated or relabeled.

This is process-clock interposition, not an independent kernel clock, NTP correction, or a deployed multi-host test.
The successful executor fixture does not establish destination deduplication or capacity.
The clock-specific work subsequently passed independent acceptance as recorded below.
Dockerfile primary LSP checks timed out and are not claimed clean. Shell and Go primary checks were clean, and the actual image build passed.

## Evidence and remaining work

All local artifacts are under `out/remediation`:

- `r36-unit-coverage-results.json` and `r36-unit-coverage-summary.json`: initial command, environment, hashes, and analysis.
- `r36-gap-tests-results.json`: repeated delayed-claim and PostgreSQL concurrency runs.
- `r36-coverage-after-gaps-results.json`: full-root and tagged PostgreSQL race/coverage commands after the new tests.
- `r36-unit-postgres-coverage-summary.json`: merged counts, per-file/package results, exclusions, and profile hashes.
- `r36-unit-function-coverage.log` and `r36-unit-postgres-functions.log`: standard Go function reports.
- `r36-clock-source.json` and `r36-clock-image-build.json`: pinned source, image identity, build result, and diagnostic limits.
- `r36-clock-fixture-probes.json`: redacted fixture ownership and observed SQL offsets.
- `r36-clock-initial-results.json` and `r36-clock-race-results.json`: both clock signs, repeat counts, and expected negative probes.
- `r36-clock-full-results.json`: full-root and tagged PostgreSQL race results with JSON skip-event classification.
- `r36-clock-entrypoint-signals.json`: initial red and corrected signal observations.
- `r36-clock-corrected-results.json` and `r36-clock-corrected-fixture-probes.json`: rebuilt image, current entrypoint hashes, new containers, and repeated clock tests.
- `r36-clock-fixtures.json` and `r36-clock-fixtures-corrected.json`: local mode-0600 fixture state with credentials. Neither is a publishable review artifact.

Reviewer run `68e88dcc-1d0b-4d40-9547-90b8bc4ef277` accepted the two tests and coverage interpretation with no findings.
The complete delivered report is preserved as `r36-slice-accepted.md`.
This is partial-slice acceptance, not whole-task R36 acceptance.

Before recording it, the parent verified the six scope files, 17 references, 19 evidence files, four coverage profiles, and the 23-member archive.
The patch and archive also matched their original manifest hashes.
`r36-slice-acceptance-verification.json` records those checks and unchanged HEAD/main, empty index, and protected-script integrity.
The reviewer inspected source and evidence but ran no commands or tests.
This acceptance paragraph is a later audit amendment. The original snapshot and reports remain unchanged.

## Whole-task R36 acceptance

Reviewer run `db23018e-617e-403a-8389-65c9304cb858` accepted the clock slice and combined whole-task R36 with no findings.
The complete native-delivered report is preserved as `r36-accepted.md`.
The reviewer inspected source and evidence only. It ran no commands, read no credential-bearing fixture state, and did not recompute hashes or profiles.

During review, formatters changed the shell case/redirection layout and wrapped two Python assertions.
The parent preserved the original snapshot and proved identical POSIX `shfmt` output and Python ASTs without position attributes.
The current Python fixture suite passed again.
The superseding `r36-clock-review-manifest-format-update.json`, full current patch, archive, and equivalence proof identify the accepted bytes.
A repeated notification corresponded to no further byte drift.
Corrected-image execution remains tied to its pre-format entrypoint bytes plus that explicit equivalence proof, not a new image build.

Before recording acceptance, the parent verified all 32 source/reference files, 39 original evidence files, the full patch, archive members, and formatting proof.
`r36-acceptance-verification.json` records these checks and unchanged HEAD/main, empty index, and protected-script integrity.
This section and the checklist changes are later audit amendments. All original review artifacts remain preserved.

R37 subsequently passed its separate benchmark review. The remaining R38 external gates, R39 final correctness/usability review, and R40 reconciliation remain open.
No statement percentage is a capacity estimate or a production-readiness conclusion.
