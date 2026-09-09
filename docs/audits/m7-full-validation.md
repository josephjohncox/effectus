# M7: Available validation and remaining external gates

## Status and scope

R01–R38 are independently accepted. R38 acceptance covers available validation and the explicit external-gate boundaries below.
R39 and R40 remain open.

This report records parent-executed local checks and three validation fixes. It does not establish remote CI, deployment, production readiness, or capacity.
The parent preserved the original failures and later results under `out/remediation/r38-*`.
No production Go API, protobuf identity, migration, accepted runtime test, inventory, or surface budget changed in this slice.

The checkout remains on `main` at `673d6c9d87c5aa69ce88306ca424c6788d9e3de2`.
Changes remain local and unstaged. The protected pre-existing release-script edit remains unchanged.

## Validation fixes

### Explicit CI migration mode

The durable PostgreSQL job invoked `go run ./cmd/effectusd --database-migrations=apply`.
The default serve mode rejected that command before database access:

```text
serve mode requires exactly one of --bundle or --oci-ref; use --mode=migrate for migrations
```

CI now includes `--mode=migrate`.
`TestDocumentedCIMigrationCommand` executes the checked-in shell command with a fake Go recorder.
It checks arguments and the fixture DSN environment without opening a database.
The new test failed against the original command and passed three race repetitions after the correction.
The corrected command also passed against the original, ownership-verified PostgreSQL fixture.

Evidence: `r38-ci-migration-initial-red.*`, `r38-ci-contract-red.*`, and `r38-ci-migration-fix-results.json`.
The original CI file remains in `r38-ci-before.yml`.

### Exclude local tool installations from protobuf inputs

Buf included 12 well-known protobuf files from the local Python virtual environment under `.tools/`.
They conflicted with imported well-known types. The original lint, format, and compatibility gates failed.

The only Buf configuration change adds `.tools/` to the existing build exclusions.
Lint rules, breaking rules, and the six repository protobuf files remain unchanged.
`buf ls-files` now returns exactly those six tracked files, rather than six repository files plus twelve tool files.

`TestDocumentedBufInputScope` uses real Buf and the real configuration in a temporary workspace.
It checks that `.tools/` is excluded while both `effectus/v1/` and `runtime/` remain included.
The test does not resolve imports. It failed before the configuration change and passed three race repetitions afterward.

Without Buf, the optional test skips. `EFFECTUS_REQUIRE_PROTO=1` makes missing Buf a failure.
CI selects that required mode after tool setup. The recorded local runs also selected it.

Evidence: `r38-static-gates.json`, `r38-buf-files-before.log`, `r38-buf-scope-red.*`, `r38-buf-scope-fix-results.json`, and `r38-protobuf-input-scope.json`.
The original configuration remains in `r38-buf-before.yaml`.

### Replace the moving TLC prerelease with a stable verified release

The original CI URL selected `v1.8.0` with a fixed SHA256.
[Upstream documents](https://github.com/tlaplus/tlaplus#overview) that each master commit replaces assets in that prerelease.
The downloaded jar did not match CI's expected hash. The parent stopped before Java execution.

| Artifact | SHA256 |
| --- | --- |
| Original CI expectation | `dbcc75552f21978a4846688b8e23be1a6b6c0b3fcee35d78fec2df167958ec94` |
| Downloaded rolling prerelease, not executed | `b658b4e504fdf0b721caf7066320f6b6fe5805f4dd2f717d0e47baba4097205e` |
| Selected stable v1.7.4 jar | `936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88` |

CI now selects the [stable v1.7.4 release](https://github.com/tlaplus/tlaplus/releases/tag/v1.7.4), not a new hash for the moving prerelease.
The parent verified its official published SHA1 and the independent SHA256 in [NixOS 24.11](https://github.com/NixOS/nixpkgs/blob/release-24.11/pkgs/applications/science/logic/tlaplus/default.nix).
The SHA1 comparison supplements that SHA256 evidence. It is not the CI integrity guard.
The stable release API marks it as a non-prerelease. A fixed checksum still fails closed if upstream changes or removes the asset.

`TestDocumentedTLCInstallPinAndFailure` runs the actual CI shell program with fake download, checksum, and privileged-install commands.
It checks the exact stable URL and hash, successful launcher installation, and early termination after download or checksum failure.
The test failed against the original URL. Three race repetitions passed after the correction.
No test downloads a jar, invokes real `sudo`, or writes the absolute installation paths.

Separately, real `sha256sum --check` accepted the stable jar and rejected an owned one-byte-corrupted copy.
Neither the corrupted copy nor the rolling prerelease ran as Java code.
Only the independently verified stable jar ran the models below.

Evidence: `r38-formal-download-blocker.json`, `r38-tlc-stable-source-check.json`, `r38-tlc-pin-decision.json`, `r38-tlc-contract-{red,green}.*`, and `r38-tlc-real-checksum-results.json`.
Public release metadata, the Nix source, downloaded jars, and logs remain preserved.
The failed initial driver assertion is historical evidence, not a model-check failure.

## Actual local gates

### Tool and environment boundaries

The host is Linux arm64. The default Go toolchain was Go 1.26.7.
R38 also downloaded and executed the declared Go 1.25.13 toolchain. This is not execution with the bare `go 1.25.0` directive version.

| Tool | Actual local version or selection | Boundary |
| --- | --- | --- |
| Go | 1.26.7 and 1.25.13 | Commands below distinguish them |
| Buf / SQLC | 1.50.0 / 1.29.0 | Match CI pins |
| Go protobuf plugins | 1.36.11 / 1.6.0 | Match CI pins |
| golangci-lint | 2.8.0 | CI specifies 2.7.2 |
| SQLFluff | 4.3.0 | CI specifies 3.5.0 |
| Node / npm | 26.1.0 / 11.13.0 | Not CI's Node 22 environment |
| Python | Existing absolute Python 3.14.6 virtual-environment executable | Not CI's Python 3.12 environment |
| Helm | 4.2.0+g0646808 in frozen preflight | Not CI's Helm 3.17.3. No per-command version capture |
| kubeconform | Native binary built from module v0.6.7 | Not the CI Docker image |
| govulncheck | 1.7.0 | Live source scans, not deployed-binary scans |
| TLC | Stable v1.7.4 jar, verified SHA256 | OpenJDK 26.0.1, one worker, requested 2 GiB heap |

`r38-tool-preflight.json` preserves the probes. The native kubeconform version comes from its saved Go build information.
These local results are not an exact reproduction of every pinned CI environment or architecture.

The frozen preflight records Helm `v4.2.0+g0646808` at `/home/linuxbrew/.linuxbrew/bin/helm`.
The original report and chart receipt incorrectly labeled it `4.3.0`.
No frozen per-command version or binary digest establishes `4.3.0` for the chart runs.
The chart commands invoked `helm` through `PATH`.
The corrected receipt reports the preflight version, not a new runtime measurement.
No Helm command was rerun for this correction.

The original receipt remains unchanged.
`r38-chart-results-version-correction.json` supersedes only its Helm label and records this evidence boundary.
All 13 command records and their log hashes remain unchanged.

### Go, PostgreSQL, and real Kafka

The full-root race run used:

```bash
GOTOOLCHAIN=go1.25.13 go test -race -count=1 -timeout=90s -json ./...
```

The parent also set `EFFECTUS_REQUIRE_DOCS=1`, `EFFECTUS_REQUIRE_PROTO=1`, and the absolute `EFFECTUS_EXAMPLE_PYTHON` executable.
Buf was on `PATH`. Database, broker, and clock-test selection variables were absent from this run.
All tests passed. There were zero test skips. All 13 package skips were verified as `[no test files]`.
This run included the executable embedded tutorial and authenticated Go/Python client tests.
It does not execute the standalone Compose script.

The integration race run used the same Go toolchain:

```bash
GOTOOLCHAIN=go1.25.13 go test -race -count=1 -p 1 -tags=integration \
  -timeout=90s -json ./runtime/... ./schema ./cmd/effectusd ./internal/daemon/kafka
```

The parent supplied the original owned PostgreSQL fixture and the new owned Kafka broker through subprocess environment variables.
All tests passed. There were zero test skips. The one package skip was `runtime/internal/db`, which has no test files.
This command ran PostgreSQL and Kafka tests separately. It did not exercise a combined Kafka→daemon→PostgreSQL path.
The `clockskew` tag was not selected. Earlier R36 clock-test evidence remains unchanged.

The real Kafka test also passed once normally and three times with the race detector under Go 1.26.7:

```bash
go test -v -count=1 -tags=integration -timeout=90s \
  ./internal/daemon/kafka -run '^TestKafkaConsumerGroupCommitAndRestart$'
go test -race -v -count=3 -tags=integration -timeout=90s \
  ./internal/daemon/kafka -run '^TestKafkaConsumerGroupCommitAndRestart$'
```

Each run used a fresh topic and consumer group. The test checks committed records do not replay after consumer restart.
It uses an in-memory attempt tracker and a callback, not a daemon process or business executor.
The selected broker image matches CI's digest-pinned Redpanda v24.2.10 image and supports native arm64.
The new container exposes only a loopback Kafka port, runs as `redpanda`, drops all capabilities, and forbids privilege gain.

Evidence: `r38-go125-root-race.json`, `r38-go125-integration-race.json`, `r38-kafka-results.json`, and the image/readiness receipts.
The full Go runs preceded the TLC regression addition. Its later focused race runs are separate evidence, not retroactive full-suite coverage.

### Build, static checks, and compatibility

| Check | Actual result |
| --- | --- |
| `just build` | Passed. All 16 Go files in `gen/` and `runtime/internal/db/` remained byte-identical |
| `just lint` | Passed after the Buf input-scope correction |
| `buf format -d --exit-code` | Passed after the correction. No formatting write |
| `scripts/check-buf-breaking.sh .git#branch=main` | Passed against local checkpoint `main`, without ref changes or a fetch |
| SQLC `vet` from `runtime/` | Passed |
| SQLFluff lint of migrations 10002–10004 | Passed with installed SQLFluff 4.3.0 |
| Actionlint on CI, docs, publish, and recovery workflows | Passed before the TLC pin edit. Later focused checks cover that edit |

The generation comparison used the exact current accepted bytes, not HEAD's older protobuf files.
The parent archived the pre-generation files. No inventory was regenerated.
All original 173 R36 Go-input hashes also remained unchanged after the VS Code dependency installation.

### VS Code and dependency checks

From `tools/vscode-extension/`, these commands passed: `npm ci`, `npm audit --audit-level=moderate --json`, `npm test`, and `npm run package`.
The package manifest and lockfile remained unchanged.
The live npm audit reported zero vulnerabilities at every severity.
This validates compilation, lint, unit tests, and VSIX packaging. It is not an interactive VS Code UI test.

Module discovery returned only the root Go module.
`govulncheck ./...` reported no vulnerabilities with each selected Go toolchain, 1.26.7 and 1.25.13.
These are the recorded live results, not a guarantee about later advisories or deployed artifacts.

Evidence: `r38-vscode-results.json` and `r38-go-dependency-audit.json`.

### Local release-script tests

The release-preflight, recovery-bundle-layout, reproducible-archive, and compatibility-proxy test wrappers all passed.
They did not execute the protected release-preflight script against real GitHub or registry services.
Git, GitHub, and registry commands in that test use fakes.
The compatibility test uses a local `file://` proxy and owned temporary Go caches.
Archive/layout operations remain inside owned temporary directories.

No real staging, commit, ref update, fetch, publication, or deployment occurred.
The protected release-script SHA256 stayed `3ee2921f9f14c5f32ad73512cf2ea718ea897ffb9037c963cc214b5386a7599b`.
Evidence: `r38-release-local-results.json`.

### Helm and Kubernetes schema contracts

Helm lint and template passed for all three `charts/effectusd/test-values/` fixtures.
Native kubeconform validated their three, two, and two rendered resources respectively, with zero invalid, error, or skipped resources.
The documented rollout, replica, resource, HTTP, OCI-digest, and rotation-marker assertions passed.
The shared runtime/migration Secret case failed as expected. Each of the three schema-typo cases also failed as expected.

These were local render and schema checks. Kubeconform can fetch public schemas.
No Kubernetes context, install, upgrade, or deployment was used. The CI Docker-based checker was not executed.
Evidence: `r38-chart-results-version-correction.json` and the original `r38-chart-results.json` render/schema logs.
The version-only amendment above corrects the original receipt's unsupported Helm label.

### Finite formal models

The parent copied the exact four model/configuration files into an owned hidden directory.
It selected owned metadata directories, one TLC worker, a 2 GiB heap, and a 65-second external deadline per model.
Neither deadline fired. Both runs exited zero and reported completed model checking without errors.

| Model | Generated states | Distinct states | Queue remaining | Search depth |
| --- | ---: | ---: | ---: | ---: |
| Saga | 73,909 | 2,202 | 0 | 28 |
| GenerationSwap | 28,915 | 17,484 | 0 | 22 |

The model and configuration hashes remained unchanged.
The configurations disable deadlock checks and include no temporal liveness property.
TLC's fingerprint-collision estimates remain in the logs. This is not a proof of the production implementation or external commits.
The generation-publication model does not establish daemon hot reload.
Evidence: `r38-formal-results.json` and `r38-tlc-{Saga,GenerationSwap}.log`.

## Checks after the fixes and initial report

With Go 1.25.13, the complete changed guardrails package passed a race run with required documentation, Buf, and Python selections. No tests skipped.
Repository guardrails, strict MkDocs, full `go vet ./...`, changed-package golangci-lint, and actionlint on the amended CI workflow passed.
The STE advisory command and whitespace diff check also exited zero. STE exit zero does not mean no writing advice.
Evidence: `r38-final-gates.json` and its logs.

Primary diagnostics confirmed all ten R38 source/documentation files clean before these gates.
A later cached diagnostic query returned no issues for seven dispatched scope files. It was not a fresh project-wide scan.
This section is later audit prose. It does not change the earlier source/test chronology.

## Retained state and chronology

All five PostgreSQL fixtures and the new Kafka fixture remain retained.
The parent rechecked their exact identities, ownership labels, running state, image identities where recorded, and loopback bindings.
SQL probes reconfirmed the original database clock and both pairs of ±24-hour fixtures.
Credential-bearing fixture files remain local mode-0600 state. They are not publishable evidence or reviewer inputs.

A verification probe initially treated the clock-fixture arrays as objects and stopped before those checks.
The corrected probe used the actual array structure. It did not repeat the completed checksum checks.
See `r38-retained-fixtures-verification.json`.

The original R36 coverage profiles and R37 benchmark samples remain historical evidence.
They were not regenerated or relabeled as measurements of later changes.
Interrupted turns resumed from saved results. Completed gates were not repeated merely because a turn was cancelled.

## Independent R38 acceptance

Reviewer `d27384f4-b9aa-4b72-8e9a-5d4934604db5` returned **R38: ACCEPT** and **Merge verdict: OK for R38 only**.
No outstanding findings remain. The reviewer accepted the version-evidence correction described above.
The review covered the bounded diff, named source contracts, regression tests, reports, and selected raw evidence.
It was read/search-only. Execution, numerical results, checksum comparisons, archive checks, fixture state, and SQL observations remain parent evidence.

The parent preserved the native report as `out/remediation/r38-accepted.md`.
Acceptance refers to `r38-review-manifest-version-update.json`, SHA256 `b8f2246efef8a5a72f17a95e06b3ffed00a5eeb682442dfe57b9d004301f9ae9`.
The parent verified 10 scope files, 48 references, 126 evidence files, and all 58 snapshot members before these status updates.
`r38-acceptance-verification.json` records the report provenance, artifact checks, and unchanged checkout/protected-script state.
These later status updates do not replace the frozen review or earlier evidence.

The verdict does not accept R39, R40, whole-remediation completion, remote CI, deployment, production readiness, or capacity.

## Gates not established

- [ ] **Standalone Compose first-run/restart path.** Its existing `run.sh` creates a service stack and removes containers and volumes on failure. That conflicts with current deployment and retention restrictions. Do not run it unchanged. This gate needs explicit stack/cleanup authorization or an approved retained-fixture procedure. The required inputs are a fresh owned Compose project, free loopback ports, a generated mode-0600 bundle, migration completion, and replay/conflict assertions.
- [ ] **Combined Kafka→daemon→PostgreSQL→business-commit path.** The real broker test is narrower. This gate needs an authorized isolated stack, configured daemon Kafka ingress, a reachable checked executor, dedicated identities, and durable business-commit/replay assertions.
- [ ] **Remote CI on the final source.** Local changes are intentionally unstaged and unpushed. This needs an approved commit/PR, repository runner authorization, and the configured CI tool versions and architecture. Local version differences above remain explicit.
- [ ] **Deployed Kubernetes, registry publication, and production recovery/capacity.** No target, credentials, deployment authorization, or rollback/cleanup approval was supplied. Rendered manifests, fake registry tests, and bounded benchmarks do not establish these gates.
- [ ] **Universal analyzer cleanliness.** Earlier Dockerfile primary timeouts and silent auxiliary-analyzer results are not clean results. Targeted checks and cached diagnostics must not be described as a fresh project-wide scan.

R38 acceptance leaves these external gates unchecked. It does not establish whole-remediation completion.
