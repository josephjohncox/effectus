# Codebase review at 388c3cb

## Status and scope

This document preserves the audit before remediation. It is a historical assessment, not the current product contract.

The reviewed HEAD was `388c3cb23943fc9032bcf37a18277688596972d6` on `main`.
The working tree already contained changes to `.github/scripts/release-preflight.sh`.
The audit did not change that file. Remediation must preserve those changes.

The user requested a review of correctness, effectiveness, API design, usability, documentation accuracy, learnability, and related concerns.
The user then requested durable findings, remediation tasks, and execution of those tasks with continued correctness and usability checks.

Use [the remediation checklist](../REMEDIATION.md) for current status. Original line numbers below refer to the reviewed revision.

## Overall assessment

Verdict: the reviewed implementation was not ready for production use with durable side effects.
Overall score: **5/10**. Confidence: **high** for the source-backed contradictions.

Immutable generations, checked IR, durable admission, outbox dispatch, fencing, and explicit idempotency boundaries form a sound design.
Compiler/runtime contradictions, recovery behavior, and unclear result semantics undermine that design.

| Area | Score | Assessment |
| --- | --- | --- |
| Correctness and reliability | 4/10 | Accepted programs can fail admission or evaluation. Recovery and replay need stronger contracts. |
| Architecture and effectiveness | 7/10 | Good separation and durable identity, but incomplete execution semantics. |
| API design | 4/10 | Large surface, speculative protobuf contracts, unclear results, sparse reference documentation. |
| CLI and operations | 5/10 | Normal paths work. Help, errors, shutdown, and output writes have unsafe edges. |
| Documentation accuracy | 4/10 | Useful guarantees and runbooks coexist with contradictory published pages. |
| Learnability | 4/10 | The demonstration works, but does not teach the language or integration model. |
| Testing and CI | 7/10 | Broad gates and integration infrastructure, but cross-layer behavior has gaps. |
| Performance evidence | 3/10 | No Go benchmarks were found. Scaling behavior was not established. |

Scores are qualitative judgments, not measured reliability or service-level guarantees.

## Correctness findings

### F01: Checked function calls cannot execute

Severity: high. Confidence: high.

- `compiler/checked.go:781-791` emits function-call IR.
- `ir/check.go:651-679` accepts declared pure and total functions.
- `runtime/checked_workflow.go:322-323` rejects every function call during evaluation.
- Compiler tests accept `lower(order.id)`, but do not execute that predicate.
- Theory pages describe pure predicate functions without a clear supported-runtime restriction.

Implement an immutable function resolution contract or reject unavailable functions before a generation can serve requests.
Test every accepted expression through compilation, dry-run, admission, execution, serialization, and recovery where applicable.

### F02: Collection fact types fail admission

Severity: high. Confidence: high.

- `ir/types.go` accepts generic and named list/map types.
- `compiler/checked_test.go` declares `order.tags: list<string>`.
- `runtime/durable_admission.go:104-162` validates scalars and objects, but not lists or maps.

Generic collections become unknown types. Named collections fall through as invalid values.
Nested collection fields also fail. Share type semantics across checking and admission, and test invalid elements and nested values.

### F03: Cancellation can prevent disposition after an external commit

Severity: high. Confidence: high in the context mismatch. Duplicate-dispatch consequences require fault-injection verification.

`schema/saga_dispatcher.go:75-169` invokes a destination, then writes completion with the original caller context.
That context may be canceled even when the destination committed and returned success.
A failed completion write can leave dispatch state ambiguous or reclaimable.

Use a bounded post-invocation disposition context with lease authority checks.
Test cancellation after commit, lost completion writes, lease expiry, unknown outcomes, and sink deduplication.
Do not claim destination-level exactly-once behavior.

### F04: Recovery batch leases can expire before use

Severity: high. Confidence: high.

- `runtime/recovery.go:46-109` leases a batch and executes it serially without renewal.
- `cmd/effectusd/main.go:169-222` configures 32 items and a 30-second lease.
- Existing recovery tests cover one fast execution.

Slow early executions can expire later leases. Long executions can lose terminal-write authority.
Use bounded lease acquisition or renewal, and test long-running work plus competing workers.

### F05: Terminal replay changes failure semantics

Severity: high for API correctness. Confidence: high.

- `runtime/engine.go:157-233` returns every terminal record with a nil error.
- Terminal states include failed and blocked states in `schema/ledger/contracts.go:27-34`.
- An initial failed attempt can return an error, while its replay does not.
- `runtime/execution_grpc.go:108-116` derives `success` from `DurablyAccepted`.

A gRPC result can therefore contain `success=true` and `metadata.state=failed`.
Define separate admission and terminal-completion contracts. Preserve admission-only replay behavior intentionally, not accidentally.
Use typed terminal errors and first-class response state rather than an ambiguous Boolean plus metadata.

### F06: HTTP shutdown does not drain handlers

Severity: high for operations. Confidence: high.

`cmd/effectusd/main.go:71-133` starts an HTTP server without retaining it for shutdown.
Signal cancellation returns from `run`, then closes the engine and database without `Server.Shutdown`.
Active admission can race dependency closure. Startup failures after listener creation also need cleanup.

Stop admission, cancel workers, drain handlers with a deadline, join service goroutines, then close dependencies.
Test signal and partial-startup failure paths.

### F07: HTTP errors misclassify server failures

Severity: medium-high. Confidence: high.

`cmd/effectusd/main.go:366-372` maps nearly every error to HTTP 400 and returns raw error text.
Storage failures, internal invariants, and unavailable dependencies need different statuses and sanitized bodies.
Use shared typed error categories across HTTP and gRPC. Test retryable dependency failures and internal-error redaction.

### F08: Chunked bodies bypass the intended size check

Severity: medium. Confidence: high.

`ContentLength` checks miss unknown-length requests.
`decodeJSON` uses `io.LimitReader` without explicit overflow detection.
A valid JSON value inside the limit can hide additional bytes outside it.

Use an overflow-aware limit on all JSON routes. Test chunked bodies, exact limits, unknown fields, multiple values, and trailing bytes.

### F09: Admission identity does not enforce generation labels

Severity: medium-high. Confidence: high.

`runtime/engine.go:241-320` accepts caller-provided ruleset/version without comparing them with the selected immutable generation.
`runtime/durable_admission.go:63-64` persists those labels beside that generation's artifact.
Direct callers can mislabel audit records and identities. Reject mismatches before durable writes or executor calls.

### F10: Equivalent merge defaults hash differently

Severity: medium. Confidence: high.

`runtime/engine.go:458-475` hashes the raw merge policy.
`runtime/durable_admission.go:38-40` later changes an empty value to `merge`.
Omitted and explicit defaults can therefore conflict. Normalize semantic defaults before hashing and avoid mutating caller input.

### F11: Cached execution records can hide durable state changes

Severity: medium. Confidence: high in the stale-cache path. Deployment consequences need multi-engine tests.

`runtime/engine.go:321-374` prefers cached records over a newer ledger record.
An optimistic conflict when entering running is ignored, while later writes use the old revision.
A second engine can leave a first engine with stale accepted/running state after durable completion.

Reconcile ledger revisions and terminal state safely. Test two engines, recovery, concurrent replay, and bounded cache/resource retention.

### F12: Recovered generation resources are not closed

Severity: medium. Confidence: high.

`runtime/artifact_resolver.go:33-94` creates historical generations with owned closers.
`runtime/engine.go:337-374` retains them per execution.
`Engine.Close` closes only the active generation at `runtime/engine.go:104-117`.

Track historical generation ownership by digest. Close resources exactly once after their final user, including failed and concurrent resolution paths.

### F13: Executor response parsing accepts trailing data

Severity: medium. Confidence: high.

`invocation/http.go:118-128` decodes one result without requiring EOF.
Malformed trailing data or a second JSON value can be recorded as success.
Reject protocol violations and classify ambiguous destination outcomes conservatively.

### F14: Executor adapter trusts an unverified argument hash

Severity: medium. Confidence: high.

`executorhttp/handler.go:88-98` requires an argument hash but does not compare it with the body.
A destination that relies on that metadata can receive inconsistent identity and content.
Recompute the canonical argument hash, compare it before business invocation, and test mismatches and canonical equivalence.

## API and usability findings

### F15: Public protobuf contracts exceed supported behavior

Severity: medium-high. Confidence: high.

`effectus/v1/execution.proto:12-25` publishes eight execution-service RPCs. Only `ExecuteRuleset` is implemented.
The gRPC guide names four unsupported management methods but omits streaming and two schema methods.
`facts.proto` and `verbs.proto` also publish registry services that the daemon does not register.
Buf uses FILE compatibility checks, so speculative contracts create compatibility obligations.

Preserve existing wire identities while marking reserved/deprecated surfaces explicitly.
Publish a complete capability matrix and test unsupported methods. Do not delete existing wire fields or renumber them.

### F16: The convenience gRPC constructor cannot succeed

Severity: medium-high. Confidence: high.

`runtime.NewRulesetExecutionServer` passes empty options.
Normalization requires explicit authentication, transport policy, ruleset, and version.
The wrapper allocates a listener before it inevitably fails.

Provide an honest constructor contract without relaxing security defaults. Validate options before network allocation.
Deprecate misleading compatibility helpers when required, and document the usable path.

### F17: Namespace semantics differ between transports

Severity: medium. Confidence: high in behavior. The earlier claim that the current gRPC guide required namespace was not verified.

The gRPC facade defaults a blank namespace to `default`.
HTTP only falls back to the legacy `universe` field, while embedded execution requires namespace.
Define one explicit namespace requirement for new callers. Document and test any retained compatibility behavior.

### F18: CLI output and flag handling have unsafe edges

Severity: medium. Confidence: high.

- `effectusc check` advertises and ignores `--output`.
- `compile` can overwrite its own source bundle with protobuf bytes.
- Output writes are not atomic.
- Subcommand help exits with status 2.
- `effectusd` can enter migration behavior because bundle input is absent.
- Daemon positional arguments are not consistently rejected.

Test exact command/flag ownership, successful help, exit codes, path aliases, output failure cleanup, and explicit migration mode.

### F19: Legacy BufIntegration remains an unsafe exported registry

Severity: medium. Confidence: moderate pending focused verification.

`schema/buf_integration.go:177-199` assumes directories inconsistent with current source generation and creates them during construction.
Registration methods lack consistent nil/context checks, retain caller pointers, and return shared mutable pointers.
A mutex does not protect data once pointers escape.

Clarify whether this is a deprecated compatibility API. Preserve imports where needed, remove hidden constructor mutations, and enforce defensive copies.
Test cancellation, nil inputs, concurrent access, and supported directory configuration.

### F20: Defaults and negative configuration are inconsistent

Severity: medium. Confidence: high.

`ir.DefaultLimits` is a mutable exported global read by zero-valued checks.
That allows process-wide behavior changes and data races.
Some constructors reject negative limits, while HTTP invocation, dispatch, and recovery silently substitute defaults.

Use immutable internal defaults returned by value. Retain deprecated symbols only for source compatibility.
Reject negative settings consistently and test zero as the documented default selector.

### F21: Public Go API documentation is sparse

Severity: medium. Confidence: high for the mechanical scan.

An AST scan excluded generated, internal, command, example, and compatibility directories.
It counted 452 exported declarations, with 166 attached documentation comments: 36.7 percent.
Aliases and constants count as declarations, so this is not a count of distinct user features.

| Package | Exported declarations | Documented |
| --- | ---: | ---: |
| bundle | 20 | 20 |
| compiler | 4 | 2 |
| embedded | 6 | 2 |
| executorhttp | 8 | 8 |
| invocation | 70 | 35 |
| ir | 30 | 21 |
| runtime | 79 | 16 |
| schema | 155 | 52 |
| schema/expression | 2 | 2 |
| schema/fencing | 23 | 7 |
| schema/ledger | 21 | 1 |
| schema/workflow | 34 | 0 |

The runtime, compiler, and schema packages lack package-level guides.
Root schema aliases duplicate the visible ledger/workflow vocabulary.
Document supported entry points, ownership, threading, contexts, error classes, state transitions, and compatibility boundaries.
Prefer narrow facades without breaking existing imports during this remediation.

## Documentation and learning findings

### F22: Published lifecycle pages contradict the implementation

Severity: high for documentation accuracy. Confidence: high.

`docs/coherent_flow.md` describes candidate activation, schema/verb refresh, hot loading, and shutdown draining.
`docs/SYSTEM_INTENT.md` promises hot reload and rollback without an explicit future-design label.
`docs/LIFECYCLE.md:37-38` says those phases do not exist.
Both conflicting pages appear in `mkdocs.yml`. Theory extension pages also retain refresh terminology.

Rewrite current architecture around one startup generation and process replacement. Label historical or theoretical claims explicitly.

### F23: The gRPC guide describes nonexistent daemon configuration

Severity: high. Confidence: high.

`docs/GRPC_EXECUTION.md:37-69` presents YAML keys, API write tokens, an authentication-disable setting, and `database.dsn`.
The daemon uses flags and `EFFECTUS_API_TOKEN`/`EFFECTUS_POSTGRES_DSN`.
Several listed limits are only configurable through the Go server-options API.

Replace fictional configuration with tested commands. Separate daemon flags from library options and state every fixed/default limit.

### F24: The conceptual learning path is missing

Severity: high for learnability. Confidence: high.

`docs/BASICS.md` is an operational summary without language syntax or types.
`docs/TUTORIALS.md` has launch commands, not an incremental tutorial, and names nonexistent `embedded.New`.
The advertised `.effx` language lacks a runnable learning path.

Teach facts, rules, flows, verbs, bindings, ordering, contracts, bundles, resolvers, diagnostics, and outcomes in that order.
Use one evolving example. Include copyable checked artifacts and a tested `.effx` flow without inventing unsupported syntax.

### F25: Contributor instructions name missing recipes

Severity: high for onboarding. Confidence: high.

`CONTRIBUTING.md` names `install-sql-tools`, `test-coverage`, `buf-format`, `buf-generate`, `sql-generate`, and `sql-validate` recipes that do not exist.
`AGENTS.md` repeats some obsolete commands.

Correct both files or implement the promised commands. Test recipe references against the actual Just command surface.

### F26: HTTP reference and route tests are incomplete

Severity: medium-high. Confidence: high.

There is no single reference for health, readiness, status, dry-run, and execute routes.
Methods, body limits, strict JSON behavior, errors, compatibility aliases, and response fields lack complete contract tests.
Status and probe handlers accept every HTTP method.

Publish the full route contract and test every method, request class, status, auth boundary, and response schema.

### F27: gRPC and Python examples lack end-to-end validation

Severity: medium-high. Confidence: high.

The Go gRPC example is compiled but not run against a matching daemon in its example recipe.
It always uses plaintext credentials and lacks a complete startup command.
`docs/tests/python_typed_facts.py` is not executed by CI.

Run an authenticated loopback acceptance path, verify idempotency, and test TLS separately.
Execute the Python example against the generated service in CI or an equivalent reproducible local gate.

### F28: Documentation contract tests are lexical

Severity: medium. Confidence: high.

Current tests mainly search for strings. They miss wrong subcommand flags, wrong defaults, broken snippets, and contradictory architecture claims.
Replace or extend them with actual help snapshots, recipe checks, runnable snippets, request validation, and local link checks.
Do not solve drift by weakening guardrails or broadly accepting stale text.

### F29: Source indexes, glossary, and release guidance are stale

Severity: medium. Confidence: high.

`docs/README.md` describes removed adapters, YAML configuration, and activation/refresh phases.
`docs/GLOSSARY.md` describes extension behavior outside the current immutable bundle boundary.
The v0.4 release note understates durable-example prerequisites as Docker Compose only.
Older release notes contain historical guarantees that must not be mistaken for current implementation status.

Align current indexes and glossary with the supported surface. Correct demonstrably false release instructions while preserving historical context.

## Testing, performance, and further validation

### F30: Cross-layer tests and runtime coverage are insufficient

Severity: high as a verification gap. Confidence: high.

The audited suite had approximately 145 ordinary test functions, two fuzz targets, and 11 integration-tag test functions across 41 files.
No benchmark functions were found in the current checkout outside ignored worktrees and dependencies.
Counts exclude subtest cases and do not measure test quality.

Coverage from ordinary `go test -coverprofile`:

- All discovered packages: 27.8 percent.
- Excluding `gen/` and `node_modules`: 40.7 percent.
- Runtime: 35.4 percent.
- Schema: 31.2 percent.
- Compiler: 68.4 percent.
- IR: 59.5 percent.

The 40.7 percent figure still includes generated SQL code and compatibility wrappers.
It is not a pure handwritten-code metric. Default package coverage also misses some cross-package exercise by examples.

Add semantic conformance tests and fault injection for each corrected behavior.
Measure coverage with explicit exclusions and explain what integration tests add.

### F31: Performance and scaling claims lack evidence

Severity: medium. Confidence: high that no benchmarks existed. Actual production throughput is unknown.

Add representative benchmarks for compilation, checking, admission, evaluation, recovery, and dispatch.
Record fixture sizes, machine/toolchain details, allocations, repeat counts, and limits.
Do not convert local microbenchmarks into production throughput promises.
Test that caches and recovered resources do not grow without a stated bound or ownership policy.

### F32: Full integration and production-boundary checks remain necessary

Severity: high as a release gate. Confidence: high.

The audit inspected PostgreSQL/Kafka/Docker CI paths but did not execute those integration suites locally.
Remediation must run available integration gates, document exact environmental blockers, and leave unverified gates open.
Add a final independent review of correctness, usability, documentation, API compatibility, and regression coverage.

## Validation observed during the audit

These commands passed on the reviewed working tree:

- `just test`
- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `just lint`
- `go vet ./...`
- `just build`
- `just test-examples`
- `go run ./examples/embedded_orders`
- `just vscode-lint`
- `just vscode-test`

A local Markdown link check found no broken local file links across 39 Markdown files.
CLI help confirmed that `check` advertises `--output` and that subcommand help exits with status 2.

## Strengths to preserve

- SourceBundle construction validates relative paths, normalizes input, sorts entries, and returns defensive copies.
- Semantic identity excludes nonsemantic metadata.
- Untrusted IR parsing enforces bounds and rejects unknown protobuf fields.
- Generation identity binds IR, environment, source, descriptors, and function identities.
- Partial resolver acquisition unwinds resources in reverse order.
- Generation.Close releases owned resources exactly once.
- Authentication, TLS, reserved headers, and signed digest-pinned OCI loading generally fail closed.
- Guarantees distinguish durable acceptance from destination success and avoid generic exactly-once or ACID claims.
- The standalone demonstration checks restart/replay identity and destination-side idempotency.
- CI covers modules, protobuf compatibility, generated bindings, SQL, Kafka restart, races, formal models, vulnerabilities, Helm, and images.
- Example assets and inventory generation reduce drift.
- The VS Code extension accurately presents its narrow syntax-support scope.

## Remediation order

1. Align compiler, checker, admission, and evaluator semantics.
2. Repair post-invocation disposition and recovery lease handling.
3. Define replay outcomes, generation identity checks, cache reconciliation, and resource ownership.
4. Correct transport, CLI, and executor-boundary contracts.
5. Clarify supported APIs and immutable defaults without silent compatibility breaks.
6. Repair documentation and add executable learning paths.
7. Add benchmarks, fault injection, coverage evidence, integration checks, and independent review.
