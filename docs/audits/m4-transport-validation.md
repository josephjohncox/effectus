# M4 transport, executor, and CLI validation

## Status

R14–R24 are implemented, tested, and independently accepted.
R01–R13 remain accepted. R25–R40 remain open.

First review: `0a95a9ec-c46e-4cb4-a9c3-08944713c077`.
Re-review: `26e3026e-03a6-4d65-a76c-6388e679cf29`.
The reviewer received a scoped baseline patch and a SHA256 source manifest.
Application code remains unchanged during each review.

The first review required one correction. HTTP returned 503 for storage outages, but gRPC returned `Internal`.
The correction maps matching gRPC dependency errors to `Unavailable` and preserves cancellation and deadline precedence.
A new regression injects ledger failures and checks both transports, cause sanitization, zero writes, and zero business invocations.

A Go overlay reproduced the three failures against the exact reviewed source hash.
Corrected tests passed with `-race -count=3 ./runtime ./cmd/effectusd`. Focused vet also passed.
Only `runtime/execution_grpc.go` changed among the previously reviewed application files.
The re-review accepted R14–R24 with no remaining required fix or regression gap in its scope.
Evidence is in `out/remediation/m4-r16-validation-results.json` and `m4-review-accepted.md`.

## Implemented behavior

- **R14:** gRPC separates durable acceptance from completion. The existing `success` field means successful completion only.
  Fields 10–13 add `durably_accepted`, `completed`, typed `state`, and `generation_digest`.
  Failed terminal calls return `FailedPrecondition` with a sanitized `ExecutionResponse` status detail.
  The interceptor preserves that detail. Replay does not invoke the executor again.
  An explicit generation constraint checks the pinned replay artifact, not only the active generation.
- **R15:** The daemon prepares listeners before it starts services. Preparation failures close owned listeners.
  Shutdown rejects new HTTP requests, cancels workers, and drains active HTTP handlers.
  At grace expiry, the server closes connections and cancels request contexts.
  It joins handlers, gRPC shutdown, and workers before closing the engine and database, in that order.
  Concurrent gRPC `Stop` calls join the same shutdown.
  Wrapped operation errors remain visible during shutdown.
- **R16:** HTTP uses typed, sanitized errors. Invalid input returns 400, identity conflicts return 409, and terminal business failure returns 422.
  Blocked terminal states return 409. Temporary dependency errors return 503.
  Cancellation returns 408, deadline expiry returns 504, and unknown internal errors return 500.
  Error responses do not expose executor causes or database error text.
- **R17:** Both JSON endpoints detect overflow, including chunked bodies and trailing whitespace.
  They reject unknown fields, trailing values, and missing fact objects.
  The decoder preserves JSON number text. All routes enforce exact paths and methods.
- **R18:** A successful executor response must contain one complete JSON value.
  Empty bodies, trailing junk, multiple values, and oversized responses produce an unknown outcome.
  Explicit JSON `null` remains a valid result.
- **R19:** The executor handler verifies the canonical argument hash before business code runs.
  Key ordering and JSON escaping normalize. Number spellings remain significant under the existing canonical JSON contract.
  Hash verification does not replace business idempotency storage.
- **R20:** gRPC constructors validate configuration before binding.
  The legacy two-argument constructor returns an actionable configuration error without binding.
  The options constructor documents authentication, transport, generation identity, defaults, and ownership.
- **R21:** HTTP and gRPC trim namespaces and idempotency keys before computing identity.
  They reject missing namespaces. HTTP retains `universe` as an alias and rejects conflicting aliases.
  A real HTTP-to-gRPC replay test verifies identical identity across nested and dotted facts.
- **R22:** Compiler help exits successfully. `check` rejects `--output`.
  The daemon rejects positional arguments and requires a source in serve mode.
  `--mode=migrate` explicitly selects migration validation or application.
  `--migrate-only` remains an explicit application alias.
- **R23:** Compiler output uses a temporary file, sync, close, and rename.
  Same-path, hardlink, and symlink aliases fail without damaging the source.
  Injected create, write, short-write, sync, close, and rename failures preserve existing output and remove temporary files.
- **R24:** Negative transport limits fail. Public HTTP executor structs also receive validation before invocation.
  Zero HTTP request and response limits select 1 MiB. `MaxInt64` fails because overflow detection needs an extra byte.
  HTTP drain grace defaults to 30 seconds. Its zero setting selects that default.
  The server also sets bounded header-read, request-read, response-write, and idle timeouts.

## Validation results

| Gate | Result |
| --- | --- |
| Full repository Go race tests | Passed: `go test -race -timeout=90s -count=1 ./...`. |
| PostgreSQL race integration | Passed for `./schema ./runtime ./cmd/effectusd` with `-tags=integration -p 1`. |
| Repeated shutdown/runtime races | Passed with `-race -count=3` for `./cmd/effectusd ./runtime`. |
| Focused `go vet` | Passed for runtime, schema, invocation, executor HTTP, and both CLIs. |
| Protobuf lint | Passed. |
| Protobuf compatibility against HEAD | Passed. Existing field numbers remain unchanged. |
| Repository guardrails | Passed after explicit inventory review. No budget changed. |
| Primary LSP diagnostics | No errors in the checked implementation and test files. |

Logs and exit records are under `out/remediation/`.
The main records are `m4-validation-results.json` and `m4-guardrails-final-result.json`.
Full-suite results preceded the classifier correction. Focused race tests and vet passed again after that correction.
The first guardrail run correctly rejected the new generated surface.
The inventory now includes 23 additive symbols and the extended response struct.
Only `gen/effectus/v1/execution.pb.go` changed among tracked generated bindings.
The repository has no tracked `clients/` bindings.

The protected release script still has SHA256 `3ee2921f9f14c5f32ad73512cf2ea718ea897ffb9037c963cc214b5386a7599b`.
No changes are staged. `git diff --check` passes.

## Limits and remaining work

Cancellation is cooperative. A callback that ignores cancellation can delay shutdown beyond the grace period.
The daemon does not close dependencies while a tracked callback still uses them.
It cannot terminate a Go goroutine safely or guarantee exactly-once effects at a remote destination.

Some auxiliary diagnostics reported incomplete coverage.
Primary LSP results do not establish that every security or static-analysis gate passed.
The operator-selected OCI verifier, CLI stderr output, and a misidentified `Context.Done` call received false-positive dispositions.

The tests above do not complete Kafka, Python, TLS example, documentation, coverage, benchmark, or final acceptance requirements.
Those gates remain assigned to R25–R40.
