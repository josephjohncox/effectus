# R37 benchmark measurements

## Status

The parent added and ran 28 benchmark cases. Independent review accepted R37 with no findings.
R01–R36 retain their earlier acceptance. R38–R40 remain open.
These measurements do not establish production capacity, deployment behavior, or an improvement over an earlier revision.

## Workloads and measurement boundaries

The fixtures are in `runtime/remediation_benchmark_test.go` and `runtime/remediation_dispatch_benchmark_test.go`.
They use production APIs without changing production code, accepted tests, public inventories, or surface budgets.

| Size | Plans | Steps per plan | Runtime payload string |
| --- | ---: | ---: | ---: |
| Small | 1 | 1 | 256 bytes |
| Medium | 16 | 1 | 1,024 bytes |
| Large | 128 | 1 | 4,096 bytes |

Each source bundle contains one `.eff` file with ordered `Review` rules.
The environment declares three facts and one verb with two string arguments and a boolean result.
Rule `i` matches when `order.risk >= i`. Runtime facts use dotted paths.
The payload consists of ASCII `x` characters. Runtime size cases change both plan count and payload size.
Compilation and checking vary plan count, not a literal payload embedded in the source.

| Benchmark | Timed work | Work outside the timer |
| --- | --- | --- |
| Checked compilation | `compiler.CompileChecked`, including its IR check, and plan/step count assertions | Bundle construction and initial compilation |
| IR checking | `ir.Check` and comparison with the initial digest | Compilation and creation of the input artifact copy |
| Dry-run evaluation | `Engine.DryRun`, result-length checks, and a loop that counts matching plans | Generation and engine construction |
| Fresh admission | One `Execute` call with `WaitAccepted` into fresh in-memory stores | Engine construction, result/plan-count checks, and engine closure |
| Terminal replay | `Execute` with the same terminal identity and result equality checks | One complete execution, executor-count checks, and engine closure |
| Recovery | One `RecoveryWorker.RunOnce` over 1, 8, or 32 accepted executions | Fresh engine, admission setup, terminal-state checks, and engine closure |
| Dispatch | One `Dispatcher.DispatchOne` for a queued step | Fresh saga/step/store/provider, outcome/attempt/metadata checks |

Dry-run cases match zero, one, or all plans. The one-plan `one` and `all` cases intentionally describe the same workload.
`DryRun` includes fact normalization, artifact copies, diagnostic predicate strings, and plan views. It is not a predicate-only measurement.
Checking includes the checker's own copying and serialization. It excludes the caller's initial artifact copy.

Each admission sample uses a new engine, ledger, and outbox. It persists one execution with the stated number of matched plans.
It must remain nonterminal and invoke no executor. These separate in-memory stores do not measure PostgreSQL atomic admission.
Each replay case retains one completed execution. Replay must not invoke the executor again.
Each recovery operation starts with exactly its named batch size and one small plan per execution.
The recovery worker uses a one-minute lease. This short successful workload does not measure sustained heartbeat renewal.

Dispatch cases use 256-byte or 4,096-byte payload strings, with zero or one in-memory fencing requirement.
Each operation starts with exactly one queued step under a stable saga identity. It must persist one successful attempt.
The fenced case checks grant count and attempt metadata. It does not measure destination-side fence enforcement.
Executor fixtures increment an atomic counter and return success. They perform no business I/O or destination deduplication.

Fresh stores bound retained state per operation instead of accumulating work across benchmark iterations.
No measured recovery or dispatch call can pass by polling an empty queue.
Fixture setup and closure occur outside the timer for successful state-changing cases.
Timer pauses exclude setup allocation counters, but do not remove later cache or garbage-collection effects of setup.
Runtime allocation counters can include other goroutines in the same benchmark process during the timed interval.
These are initialized-input measurements, not cold process-start measurements.

## Commands and environment

The measured command passed:

```bash
go test -run '^$' -bench '^BenchmarkRemediation' -benchmem \
  -benchtime=20x -count=5 -cpu=1 -timeout=240s ./runtime
```

There are five samples per case and 20 timed iterations per sample.
The Go benchmark harness also performs its initial one-iteration run before a fixed count greater than one.
The measured command uses neither race nor coverage instrumentation.
`-cpu=1` selects `GOMAXPROCS=1`. It does not pin the process to one CPU.

The actual toolchain was Go 1.26.7 on Linux arm64, with cgo enabled, `GOARM64=v8.0`, and `GOTOOLCHAIN=auto`.
The module still declares Go 1.25.0 and toolchain Go 1.25.13. This run does not validate that minimum toolchain.
The kernel reported `6.17.0-1032-nvidia`.
`lscpu` reported 20 online CPUs, an ARM Cortex-A725 model, and a 338–2,808 MHz frequency range.
The process affinity allowed CPUs 0–19. Linux reported `MemTotal: 127535256 kB`.
`GOFLAGS` was empty. `GOGC`, `GOMEMLIMIT`, and the inherited `GOMAXPROCS` were unset.

The host was not CPU-isolated or thermally controlled. Local tools and five retained PostgreSQL fixtures could add host noise.
No database participates in these benchmarks. The fixtures are not a deployed daemon, broker, or HTTP service.

## Results

Each row reports the median of five sample means. Brackets show the minimum and maximum sample means.
The time unit is microseconds per operation. Recovery reports time per complete batch, not per execution.
Allocation columns show the median reported bytes and allocation count per operation.
Allocated bytes are not retained heap size or peak memory.

| Case | µs/op median [min, max] | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Compilation / small | 278.28 [270.80, 581.25] | 406,513 | 3,413 |
| Compilation / medium | 1,100.20 [1,095.52, 1,110.82] | 966,983 | 10,718 |
| Compilation / large | 8,422.85 [8,369.02, 8,469.07] | 5,608,130 | 65,008 |
| Checking / small | 11.04 [10.45, 13.75] | 10,400 | 158 |
| Checking / medium | 133.62 [116.61, 135.20] | 70,030 | 1,527 |
| Checking / large | 946.49 [929.21, 952.81] | 518,247 | 11,629 |
| Dry-run / small / none | 15.32 [14.96, 24.91] | 31,797 | 275 |
| Dry-run / small / one | 15.39 [15.08, 15.90] | 31,886 | 281 |
| Dry-run / small / all | 15.28 [15.00, 15.67] | 31,886 | 281 |
| Dry-run / medium / none | 110.92 [110.67, 111.73] | 85,826 | 1,376 |
| Dry-run / medium / one | 112.21 [110.99, 114.65] | 86,772 | 1,412 |
| Dry-run / medium / all | 112.44 [111.09, 113.82] | 86,788 | 1,415 |
| Dry-run / large / none | 562.25 [558.51, 577.87] | 451,675 | 9,561 |
| Dry-run / large / one | 568.45 [557.95, 570.39] | 458,923 | 9,821 |
| Dry-run / large / all | 563.17 [552.48, 578.94] | 458,936 | 9,824 |
| Admission / small | 127.18 [125.16, 152.60] | 156,370 | 1,435 |
| Admission / medium | 861.25 [859.01, 864.88] | 851,860 | 12,525 |
| Admission / large | 30,179.43 [29,928.86, 30,299.42] | 27,919,733 | 536,408 |
| Replay / small | 21.53 [21.40, 29.08] | 42,544 | 305 |
| Replay / medium | 38.48 [35.75, 38.53] | 67,144 | 313 |
| Replay / large | 108.01 [102.25, 115.00] | 171,117 | 317 |
| Recovery / batch 1 | 70.16 [66.73, 74.59] | 60,572 | 537 |
| Recovery / batch 8 | 435.11 [424.85, 471.58] | 484,209 | 4,317 |
| Recovery / batch 32 | 1,795.90 [1,789.17, 1,806.43] | 1,954,334 | 17,327 |
| Dispatch / 256 bytes / no fence | 6.37 [6.08, 7.99] | 9,610 | 57 |
| Dispatch / 256 bytes / one fence | 8.11 [7.70, 8.64] | 11,041 | 80 |
| Dispatch / 4,096 bytes / no fence | 19.16 [18.87, 25.43] | 36,362 | 60 |
| Dispatch / 4,096 bytes / one fence | 20.53 [20.28, 21.06] | 37,793 | 83 |

The short fixed iteration count limits precision, especially for fast cases. No sample was discarded.
The small compilation range retains the 581.25 µs sample rather than hiding its variation.
The large admission case reports substantial allocation work. It is not evidence of a leak or a measured performance regression.
The cases change several workload dimensions and do not isolate the cause of that cost.
No earlier comparable benchmark exists in this evidence. There is no statistical significance test or before/after speedup claim.
The ranges are not request-latency percentiles, confidence intervals, or service-level objectives.

## Correctness checks and development record

These commands passed separately from the performance run:

```bash
go test -race -count=3 -timeout=90s -v ./runtime \
  -run '^TestRemediationBenchmarkFixtures$'
go test -race -count=1 -timeout=90s -run '^$' \
  -bench '^BenchmarkRemediation' -benchtime=1x -benchmem ./runtime
```

The test checks every size, match count, admission plan count, recovery completion, replay invocation count, and dispatch payload/fencing case.
The race benchmark smoke run exercises all 28 timed bodies and their assertions. Its timings are not performance evidence.
Primary Go diagnostics passed before these runs.

Initial primary diagnostics caught three fixture API mistakes: `ArgTypes`, a two-result `SourceBundle.Digest` call, and `ExecutionRecord.SagaIDs`.
The fixture now uses `Arguments`, handles the digest error, and checks `ExecutionRecord.Plans`.
No production API or assertion was changed to accept those mistakes.
The parent also moved custom metrics after `ResetTimer` before execution because that method clears reported metrics.
A direct read of the pinned Go 1.26.7 benchmark source confirmed timer and counter behavior.
One auxiliary analyzer reported `opengrep silent`. Primary Go checks do not establish that analyzer's cleanliness.

The full-root race suite then passed with required MkDocs and the explicitly selected absolute Python venv interpreter.
Its Go JSON log contains no test-level skips. All 13 package-level skip events have matching `[no test files]` output.
Guardrails, the strict documentation site, `go vet ./...`, the STE advisory, and `git diff --check` also passed.
These local checks are not remote CI or the remaining R38 external gates.

## Evidence and limits

Local evidence is under `out/remediation`:

- `r37-machine.json`: toolchain, host observations, environment, and source/module hashes.
- `r37-initial-validation-results.json`, `r37-fixture-race.log`, and `r37-benchmark-race-smoke.log`: correctness runs.
- `r37-benchmark-measurements.json` and `r37-benchmark-measurements.log`: exact measured command, exit status, and all 140 result lines.
- `r37-analyze.py` and `r37-benchmark-summary.json`: repeatable analysis, all samples, log/script hashes, and metric checks.
- `r37-gates.json` and its named logs: full-root race, documentation, guardrails, vet, advisory, and diff results.

The analysis requires exactly 28 named cases, five samples each, 20 iterations per sample, and the expected workload metrics.
It rejects missing, unexpected, or malformed results instead of silently pooling them.
The source and module hashes still matched after measurement and after the interrupted turn resumed.

These fixtures do not measure PostgreSQL, Kafka, TLS, network latency, remote executors, concurrent clients, or growing backlogs.
They do not measure retry storms, compensation, renewal under long-running work, destination deduplication, or historical-generation loading.
The dispatch fixture supplies an already queued step. It does not measure compilation, admission, or enqueue costs.
The recovery fixture supplies an already admitted batch. It does not measure a continuous service or an idle polling loop.
No result establishes another architecture, toolchain, workload distribution, or maximum supported scale.
Use separate deployment and load gates before any capacity or production-readiness claim.

## Independent acceptance

Reviewer run `55319299-8114-469e-a590-2b2af601c435` accepted R37 with no findings and a merge verdict limited to R37.
The full native-delivered report is preserved as `out/remediation/r37-accepted.md`.
The reviewer read source, reports, and receipts. It ran no commands, tests, calculations, or hash checks and accessed no fixture credentials.

The accepted freeze is `r37-review-manifest-format-update.json` under `out/remediation`.
Its SHA256 is `b1b5156921f8ab8e62887a1a848aca0ff6a03221d12dbb7b4eee1c7d8748f00f`.
The formatter amendment adds one blank line before the older R36 acceptance heading in the measurement report.
The parent built isolated frozen and current documentation copies with strict MkDocs. Their complete rendered measurement-report HTML was byte-identical.
All benchmark source and measured inputs remained unchanged. No new benchmark execution is claimed for that formatting amendment.

Before recording acceptance, the parent verified six scope files, 38 references, 20 original evidence files, and all 44 archive members.
The full patch, baseline, formatting proofs, HEAD/main, empty index, and protected-script hash also matched.
`r37-acceptance-verification.json` records those checks. The original and superseding review artifacts remain unchanged.
This acceptance section and checklist updates are later audit amendments, not part of the reviewed snapshot.
R38–R40 remain open. Acceptance does not extend to remote CI, production readiness, deployment behavior, or capacity.
