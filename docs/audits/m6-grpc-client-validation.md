# R33 authenticated client and TLS validation

## Status

R33 passed independent acceptance. The reviewer found no R33 code or test issues.
R32 and the remaining M6 tasks do not gain acceptance from these results.

## Scope

The Go client now uses the shared order-review scenario, explicit response fields, bearer authentication, and verified TLS by default.
The new Python client uses the same scenario and current generated bindings.
Both have explicit plaintext overrides, client deadlines, conflict inputs, sanitized RPC-status output, and testable CLI behavior.
Help does not print the environment token.

`examples/grpc_execution/client_test.go` runs the actual Go main function and Python program in subprocesses.
`service_test.go` creates the matching authenticated service, in-memory engine, example executor, and temporary TLS certificates.
The tests close and join the service before closing the engine.
The shared scenario and rule remain the existing `examples/order_review` assets.

The client guide and example README now describe complete prerequisites and an automated gate.
AGENTS.md and CONTRIBUTING.md no longer call the Go gRPC example compile-only.
Those two descriptions are later R33 amendments to the accepted R30–R31 snapshot, not changes to its historical evidence.

No production Go dependency, protobuf field, service identity, visible recipe, package inventory, or example budget changed.
Python requirements are pinned in the example directory.
The venv and generated Python files are ignored, not committed library bindings.
The example remains within the existing `grpc_execution` surface.

## Actual checks

Both clients passed:

- Bearer authentication rejection and local missing-token validation
- Successful terminal execution with explicit accepted/completed/success fields
- Same-language and cross-language replay with one executor call
- Changed-content conflict rejection without another executor call
- Verified TLS execution and explicit local plaintext execution
- Rejection of system-untrusted and unrelated trust roots
- Hostname verification despite trusting the certificate's issuer
- Rejection of automatic TLS-to-plaintext fallback
- Incompatible CA/plaintext options, bounded deadlines, and CLI help/error token redaction

The Python gate generates bindings from the current common and execution protos into a fresh temporary directory.
It does not reuse manually generated bindings.
Ordinary Go tests skip Python if no interpreter is selected.
Every validation command below selected the venv interpreter explicitly.

## Validation results

`out/remediation/m6-r33-results.json` records:

- `go test -race -count=3 -timeout=90s ./examples/grpc_execution`: passed with Python selected.
- Full `go test -race -count=1 -timeout=90s ./...`: passed with Python selected.
- `just test-examples`: passed with both clients selected.
- `just guardrails`: passed without inventory or budget changes.
- Fresh project Pyright with the actual venv: zero errors and warnings.
- `pip check`: passed for the pinned environment.
- STE lint: completed with advisory findings.
- Six relative file-link targets: present. This check did not verify remote URLs or anchors.
- `git diff --check`: passed.
- Empty staged set and unchanged protected release-script hash: confirmed.

The session's primary LSP checks passed for the Go files and changed documentation.
The running auxiliary Python analyzer still reports two unresolved generated imports.
Fresh project Pyright and actual Python subprocesses resolve both modules successfully.
Those auxiliary findings have explicit false-positive dispositions with evidence. They are not a blanket clean auxiliary scan.
No import diagnostic or source guard was disabled.

## Setup corrections and retained evidence

The first Python gate selected the base interpreter after the driver resolved the venv executable's symlink.
The gate correctly failed because that interpreter lacked `grpc_tools`.
The driver now preserves the absolute venv executable path. No test assertion or interpreter failure was weakened into a skip.
The initial and corrected runs remain in `m6-r33-clients-initial.*` and `m6-r33-python-client-first.*` under `out/remediation`.

An initial out-directory generation experiment used an extra import-path configuration.
The final manual setup generates ignored bindings beside `effectus/v1` and uses the repository root as PYTHONPATH.
The temporary extra-path configuration was removed.
The automated gate independently generates its own temporary bindings.

Pinned tool versions and setup output are preserved in `m6-r33-python-freeze.txt` and `m6-r33-python-setup*.log`.
The validated interpreter was Python 3.14.6. Package metadata for the pinned requirements requires Python 3.10 or later overall.
This is not a test of every compatible interpreter or operating system.

## Independent review and snapshot amendment

Reviewer run `1bfcbe9e-f673-4a98-9e88-6ab13f5be883` returned **ACCEPT for R33**, with a separate required R32 wording fix.
The complete report is preserved at `out/remediation/m6-r32-r33-review-first.md`.
The reviewer ran no commands or tests and made no source edits.
The review distinguished source assessment from the parent's test and snapshot evidence.

Three frozen files changed formatting during review:

- `client.py`: expression/call wrapping, 3,798 to 3,870 bytes
- `client_test.go`: one final blank line removed, 8,479 to 8,478 bytes
- `service_test.go`: struct-field alignment, 4,714 to 4,724 bytes

The parent reconstructed each original from the preserved patch and verified its frozen SHA256.
Python ASTs matched without source positions. Both Go files produced byte-identical gofmt output.
The other 12 scoped hashes, all 12 reference hashes, HEAD, empty index, and protected release-script hash were unchanged.
The reviewer inspected the bounded amendment before accepting R33.

Evidence remains under `out/remediation`:

- Original `m6-r32-r33-review-manifest.json` and `m6-r32-r33-review.patch`
- Superseding `m6-r32-r33-review-manifest-format-update.json`
- Exact `m6-r32-r33-format-only.patch`
- Reconstruction and equivalence evidence in `m6-r32-r33-format-drift-evidence.json`

The parent rechecked all 15 amended scope hashes and 12 reference hashes before recording this acceptance.
This acceptance prose is a later audit update, not part of the frozen review snapshot.

## Boundaries

The fixture is runnable through the automated gate, not a persistent daemon or a durable destination.
It proves the client/service contract for this scenario, not production capacity, crash recovery, or exactly-once destination effects.
The client programs print only status codes on RPC failure.
Applications that need unsuccessful durable dispositions must inspect approved response details as the guide explains.

Strict whole-site validation, the remaining behavioral documentation contracts, combined M6 acceptance, and R36–R40 remain open.
