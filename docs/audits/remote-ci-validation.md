# Remote CI validation

**Remote CI passed on the exact published source.**

The user authorized publishing the reviewed source to a new validation branch and running CI.
That authorization excluded updating `main`, opening a pull request, tagging, releasing, and deploying.

## Published source

The parent created a separate worktree and never committed on `main`.

- Branch: `validation/remediation-e566c0316db9`
- Base: `673d6c9d87c5aa69ce88306ca424c6788d9e3de2`
- Final commit: `7fd8abaaa9f85e01d311f2475689dabee749fbe7`
- Tree: 376 files, each blob verified against the reviewed source

Every commit was signed and verified locally and by GitHub.
`.github/scripts/release-preflight.sh` keeps its earlier committed bytes in the published tree.
The user's local edit to that script stays in the original checkout and was never staged or published.
Ignored tools, caches, credentials, task evidence, and helper sources were excluded.

## Three runs

| Commit | Change | Run | Result |
| --- | --- | --- | --- |
| `d3412952` | Reviewed remediation source, 109 files | 34295724895 | Failure. 14 of 15 jobs passed. |
| `a68e92ab` | `js-yaml` lock entry 4.3.1 to 4.3.2 | 34665944532 | Failure. 14 of 15 jobs passed. |
| `7fd8abaa` | `google.golang.org/grpc` 1.83.2 | 34666494590 | **Success. 15 of 15 jobs passed.** |

The first run failed only in the `vscode` job, at the `Audit npm dependencies` step.
It reported high-severity advisory GHSA-2883-xcg3-v3hh against `js-yaml` 4.3.1.
Independent review accepted the lock update. See the review record in the remediation evidence.

The second run fixed that job and then failed in `container-audit`.
Trivy reported CVE-2026-84445 against `google.golang.org/grpc` 1.83.1 in both Go binaries.
One failure had masked the next. The earlier statement that `js-yaml` was the only blocker was incomplete.

The third run passed every job and every step.

## Dependency changes

Two dependency changes were needed. Neither changed product logic.

`tools/vscode-extension/package-lock.json` moved `js-yaml` from 4.3.1 to 4.3.2.
`package.json` needed no edit, because the declared ranges already admitted the patched version.
The nested `mocha` copy stays at 5.4.1, outside the affected ranges.

`go.mod` and `go.sum` moved `google.golang.org/grpc` from 1.83.1 to 1.83.2.
That upgrade also raised four transitive modules: `golang.org/x/net`, `text`, `sync`, and `sys`.
`go.sum` also records `golang.org/x/mod` and `golang.org/x/tools` entries.
This was not a single-line change.

`google.golang.org/protobuf` stays at 1.36.11.
The `go` directive and toolchain line are unchanged.
Protobuf and SQL regeneration produced byte-identical output.

## Parent validation before publication

The parent ran these checks on the source it published.

- `go build ./...` and `go vet ./...`: exit 0
- Root race suite: 665 tests passed, no failures, no test skips
- gRPC, HTTP, and documented-contract races, three repetitions: 369 tests passed
- Guardrails, whitespace diff check, protobuf regeneration, SQL regeneration: exit 0
- `govulncheck ./...`: no vulnerabilities
- The exact CI container scan with pinned Trivy: exit 0, and both binaries reported zero findings

The local extension check mirrored the `vscode` job: install, audit, test, and package.
The audit reported zero vulnerabilities.

Local toolchains differ from the pinned CI versions.
Only the passing CI run settles the result for the published source.

## Final run detail

Run 34666494590 completed with conclusion `success` on attempt 1.
All 15 jobs completed successfully, across 133 steps, with no failed, skipped, or cancelled job.

Four security gates passed:

- `vscode` / Audit npm dependencies
- `dependency-audit` / Audit every discovered Go module
- `container-audit` / Scan fixed high and critical findings
- `example-container-audit` / Scan fixed high and critical findings

No finding was suppressed. No severity threshold, ignore file, or exit code was relaxed.
No workflow was rerun, cancelled, or dispatched.

## Scope and limits

This gate proves that the published source passes every CI job on GitHub-hosted runners.
It does not prove deployment, registry publication, cluster recovery, or production capacity.

`main` still resolves to the base commit. Nothing was merged, tagged, or released.
The [remaining gates](m7-final-review.md#residual-limits-and-unexecuted-gates) stay outside this scope.

## Retained local resources

One protected fixture was lost during this work, and the loss is recorded separately.
The original R36 PostgreSQL container and its anonymous volume no longer exist.
An earlier parent claim that all sixteen containers were unchanged was false and is retracted.
The cause is unproven, and the loss is unrecoverable.

The 15 surviving containers match their post-reboot observations exactly.
They are stopped, retained, and were not restarted for this gate.
The audit image built for the local container scan is task-owned and additive.
