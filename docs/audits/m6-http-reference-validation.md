# R32 HTTP reference validation

## Status and scope

R32 passed independent acceptance after its sole review finding was corrected. Combined M6 acceptance remains open.
The scope is `docs/HTTP_API.md`, its link from `docs/COMMANDS.md`, `cmd/effectusd/http_reference_test.go`, and `cmd/effectusd/http_auth_wire_test.go`.
No production handler, transport schema, public declaration, inventory, or budget changed.

## Reference coverage

The reference documents every current route, method, authentication rule, JSON body, and response shape.
It preserves the current mix of lowercase admission fields and capitalized generation/plan-view fields.
It describes declaration maps and nested contracts, result states, namespace/universe aliases, identity scope, generation constraints, and accepted-only waits.
It also covers request limits, sanitized errors, shutdown admission, and connection timeouts.

The reference does not invent an execution-history endpoint, per-namespace authorization, ETag parser, terminal HTTP wait, or dependency-health probe.
It calls out the unauthenticated readiness metadata and separates HTTP from inbound gRPC and outbound executor integration.

## Executable examples

The tests load the JSON examples directly from the reference and send requests over real loopback TCP.
Only marked generation-specific string placeholders may vary.
Object keys, omissions, arrays, types, booleans, declared policies, and other literal values must match the actual response.

Tests verify:

- Liveness, generation views, predicate evaluation, and nonmatching plans
- Accepted-only requests even when a query asks for terminal waiting
- HTTP 202 replay of a failed terminal identity, preserving its ID and unsuccessful disposition
- Conflicting logical content, strict bearer headers, and protected unknown routes
- Bare, quoted, empty, weak, wildcard, and list-style `If-Match` values
- Current non-enforcement of request Content-Type
- Real HEAD response suppression and ignored query parameters
- HTTP 503 after draining begins, including probes and unauthenticated paths
- HTTP 200 readiness during an injected ledger outage, with zero ledger probes, followed by sanitized 503 execution failure and no writes

Existing HTTP boundary and transport tests also ran for exact/chunked body limits, number preservation, aliases, typed errors, storage failures, and lifecycle behavior.

## Correction found by the tests

The first reference draft incorrectly rendered checked-step defaults in the declaration environment.
Real readiness JSON retained `max_attempts: 0` and an empty `idempotency_policy` for the fixture's unspecified declarations.
The reference now distinguishes those values from the execution defaults normalized by the checker.
The strict JSON assertions were retained. No handler change was used to make the draft correct.

The initial failure and observed wire example remain in:

- `out/remediation/m6-r32-http-examples-initial.log`
- `out/remediation/m6-r32-readiness-wire.log`

## Independent review correction

Reviewer run `1bfcbe9e-f673-4a98-9e88-6ab13f5be883` required one R32 correction and separately accepted R33.
Its report is preserved at `out/remediation/m6-r32-r33-review-first.md`.
The P1 finding concerned authentication wording, not a way to authenticate without the correct token.
HTTP/1 parsing trims outer spaces and tabs before the handler compares the header value.

The reference now qualifies exact comparison as applying to the parsed header.
The new raw TCP regression writes the complete request bytes without `http.Client` or `Request.Write` normalizing them first.
It verifies canonical and outer-whitespace acceptance, extra whitespace after the prefix, wrong tokens, wrong prefix case, and duplicate values.
It also verifies response JSON/challenges and zero executor calls.
Production authentication and existing assertions remain unchanged.

`out/remediation/m6-r32-review-fix-results.json` records five repeated raw-wire race runs, three repeated affected HTTP/storage race runs, guardrails, and STE lint.
All commands passed. Primary LSP checks for the correction also passed.
The earlier full race suite below preceded this documentation/test-only correction. The affected package was rerun afterward.
Re-review run `204b1bf5-815d-45ca-b33d-f77de3784455` returned **ACCEPT for R32**, with no issues.
The report is preserved at `out/remediation/m6-r32-accepted.md`.
The reviewer independently inspected source and recorded evidence but ran no commands or tests.
A repeated formatting notice prompted explicit verification: all five R32 files, twelve references, and three reported R33 files still matched.
That evidence is preserved in `out/remediation/m6-r32-re-review-snapshot-verification.json`.
The parent rechecked the R32 scope and references before recording this acceptance. This audit update follows the frozen review snapshot.

## Validation

Command and file-link results are in `out/remediation/m6-r32-results.json`.
Primary LSP evidence is in the session's diagnostic tool results.
Validation includes:

- Repeated HTTP and storage transport race tests: passed.
- Full repository `go test -race -count=1 -timeout=90s ./...`: passed.
- `just guardrails`: passed, including the documented HTTP examples.
- Primary LSP checks for the new test and reference: passed without errors.
- Seven relative file-link targets: present. Anchors and remote URLs were not checked by that file-existence test.
- STE lint: completed with advisory style findings, not a correctness verdict.

Strict whole-site validation, broader M6 review, and the remaining R34–R35 contracts remain open.
R32 acceptance does not imply acceptance of the remaining M6 tasks or production readiness.
