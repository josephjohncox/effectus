# Repository and PR audit: 2026-09-12

Snapshot taken on September 12 UTC (September 11 in America/Los_Angeles), before this cleanup PR.
Git refs were fetched with pruning; PR, workflow, release, and repository enforcement state were queried from GitHub.
This is a repository-state and maintenance audit, not a new correctness review of the entire implementation.

## Published baseline

`main`, `origin/main`, and tag `v0.5.0` resolved to `d14b94b2c8a3c4e29be36eca865e877b15398b48`.
There were eight open PRs, all dependency updates, and no open issues.

| Evidence | Result |
| --- | --- |
| [PR #74](https://github.com/josephjohncox/effectus/pull/74) | R01–R40 remediation merged at `bcd6b86`; its branch tip `96479d5` is an ancestor of main. |
| [Main CI](https://github.com/josephjohncox/effectus/actions/runs/34667786586) | Passed at `d14b94b`. |
| [Documentation](https://github.com/josephjohncox/effectus/actions/runs/34667786552) | Passed at `d14b94b`. |
| [Tag CI](https://github.com/josephjohncox/effectus/actions/runs/34667919681) | Passed at `d14b94b`. |
| [Publish](https://github.com/josephjohncox/effectus/actions/runs/34667919841) | Passed; [v0.5.0](https://github.com/josephjohncox/effectus/releases/tag/v0.5.0) was published, neither draft nor prerelease. |

The remediation checklist's closing claim that R39–R40 remained open was stale.
The validation report also incorrectly said accepted corrections remained local and unstaged after describing their publication.
This cleanup reconciles those statements and labels historical execution records without relabeling old test results.

## Branch and worktree reconciliation

The original checkout had one tracked modification: formatting in `.github/scripts/release-preflight.sh`.
Its SHA-256 was `3ee2921f9f14c5f32ad73512cf2ea718ea897ffb9037c963cc214b5386a7599b`.
The six other existing worktrees were clean. There were no stashes.
Two worktree registrations pointed at missing temporary directories. The cleanup prunes only those stale registrations after confirming their commits (`2d68f75` and `9795995`) are ancestors of main.

Commit ancestry alone overstates unfinished work because several branches were squashed or integrated with conflict adjustments.
The following mapping uses ancestry, patch equivalence, tree comparison, and `git range-diff`.

| Retained branch | Disposition |
| --- | --- |
| `validation/remediation-e566c0316db9` | Tip `96479d5` is in main through PR #74. |
| `pi/implement-compiler-cli-contracts` | `76bdbe3` is patch-equivalent to work in main (`git cherry` reports `-`). |
| `pi/implement-lossless-adapters` | `0ec535a` maps to `15013b3` in main; range-diff shows a CI hunk relocated during integration. |
| `pi/implement-operations-release` | `c87247d` maps to `db28448` in main, with integration adjustments across the overlapping operations and documentation work. |
| `pi/implement-runtime-core` | `34a8f3e` maps to `e9e9b2a` in main; registry changes overlapped earlier snapshot work. Follow-up `e331bf5` is patch-equivalent to main. |
| `pi/implement-usability-docs-examples` | `c5533ae` maps to `1e25fd2` in main, with integration adjustments. Later product-surface reductions superseded several examples and daemon paths. |
| `origin/joseph/major-refactor` / closed [PR #19](https://github.com/josephjohncox/effectus/pull/19) | Its parent tree exactly matches squash `8fe9530` from [PR #13](https://github.com/josephjohncox/effectus/pull/13); its final tree exactly matches subsequent main commit `e305936`. No tree content remains to recover from that PR. |
| Local documentation, release, and release-fix branches | `feat/github-pages-docs`, `fix/production-readiness-hardening`, `fix/release-artifact-selection`, `fix/release-chart-signature-recovery`, `fix/v0.2.1-review-remediation`, and `release/v0.2.0` are ancestors of main. |
| `buf-breaking-main` | Local protobuf compatibility baseline; not a feature branch. |

No unintegrated feature was identified in this branch review. Confidence is high for ancestry and exact-tree matches, moderate for semantically superseded work with integration adjustments.
Blindly merging the old worker branches risks reintroducing removed product surfaces and obsolete contracts.
The branch refs and existing worktrees are retained; their presence alone is not an implementation backlog.

## Dependency PR disposition

This cleanup consolidates four updates against the released baseline and retains `go 1.25.0` and `toolchain go1.25.13`.

| PR | Update | Disposition |
| --- | --- | --- |
| [#64](https://github.com/josephjohncox/effectus/pull/64) | protobuf 1.36.11 → 1.36.12 | Included. Original PR CI passed. |
| [#65](https://github.com/josephjohncox/effectus/pull/65) | pgx 5.9.2 → 5.10.0 | Included. Its older CI failed at the TLC installation step, outside pgx; validate on the current baseline. |
| [#66](https://github.com/josephjohncox/effectus/pull/66) | testify 1.11.1 → 1.12.1 | Included. Original PR CI passed. |
| [#70](https://github.com/josephjohncox/effectus/pull/70) | go-containerregistry 0.20.7 → 0.22.1 | Included. Original PR CI passed. |
| [#69](https://github.com/josephjohncox/effectus/pull/69) | Goose 3.26.0 → 3.28.0 | Deferred to a coordinated Go toolchain migration. The PR changes `go` to 1.26.0 and removes the patched toolchain pin. |
| [#68](https://github.com/josephjohncox/effectus/pull/68) | Node types 16 → 26 | Retained for extension compatibility review with TypeScript and the supported editor host. |
| [#71](https://github.com/josephjohncox/effectus/pull/71) | TypeScript 4.9.5 → 7.0.2 | Retained for coordinated compiler/linter validation on current main. |
| [#72](https://github.com/josephjohncox/effectus/pull/72) | VS Code types 1.100.0 → 1.136.0 | Retained for review against the declared VS Code `^1.74.0` engine contract. |

The dependency resolution also updates Docker CLI, compression, and logging modules, adds the selected test dependencies, and removes unused requirements.
Generator pins remain unchanged; a protobuf runtime patch does not itself require a generator upgrade.

The [Goose CI run](https://github.com/josephjohncox/effectus/actions/runs/34667401134) gives concrete reasons to defer it:
the pinned linter was built with Go 1.25, both Docker builds reject the Go 1.26 requirement under `GOTOOLCHAIN=local`, and the selected standard library fails the vulnerability gate.
A toolchain migration must update and validate the linter, Docker images, modules, and vulnerability scans together.

The three extension PRs have older failed `vscode` and `formal` checks.
The inspected [TypeScript run](https://github.com/josephjohncox/effectus/actions/runs/33992886548) failed at locked dependency installation and TLC installation, before compiler validation.
Those failures do not establish that the proposed TypeScript compiler is incompatible.
Rebase or recreate the updates against current main, then run installation, audit, compilation, lint, tests, and VSIX packaging together.

[PR #67](https://github.com/josephjohncox/effectus/pull/67) (gRPC) and [PR #73](https://github.com/josephjohncox/effectus/pull/73) (`js-yaml`) were already closed; their fixes shipped through PR #74.
This cleanup supersedes the four consolidated dependency PRs. Closing those duplicates does not mean their updates have shipped; that requires merging this cleanup.

## Remaining work

1. Validate deployed Kubernetes startup, migration, replacement, signature enforcement, and recovery. Release registry publication has passed; cluster behavior has not been established by that workflow.
2. Measure production capacity with representative workloads, destinations, resources, and explicit acceptance criteria. Existing benchmarks retain their original bounded scope.
3. Complete the Goose toolchain migration and coordinated extension dependency review described above.
4. Decide repository enforcement policy. GitHub's main-branch protection endpoint returned `Branch not protected`, and the repository rulesets endpoint returned an empty list. Passing CI currently does not establish enforced merge requirements.
5. Review redundant CI work separately: `race` and `runtime-race` run the same command, and unfiltered push plus pull-request triggers duplicate branch validation. This cleanup preserves existing check names and workflow behavior.

## Cleanup validation

Local checks passed with `GOTOOLCHAIN=go1.25.13`:

- `just test-modules` (the reviewed inventory currently discovers only the root module), `go vet ./...`, and `just guardrails`.
- `go test -race ./runtime ./schema ./cmd/effectusd ./internal/daemon/kafka`.
- `just lint` and `go mod verify`.
- `govulncheck ./...` using govulncheck 1.7.0: no vulnerabilities found.
- `just docs` and the required `EFFECTUS_REQUIRE_DOCS=1` uncached `DocumentedStrictSite|DocumentationCopy` renderer regressions.
- Pinned protobuf generation followed by `git diff --exit-code -- gen`: no generated changes.
- Release-preflight and recovery-bundle-layout shell regressions; `git diff --check`.

Tagged PostgreSQL/Kafka integration, container scans, and extension packaging are left to the cleanup PR's CI, whose results belong to that PR's commit.
The published-baseline workflow results above apply to `d14b94b`, not automatically to this branch.
