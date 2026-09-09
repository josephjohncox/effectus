# R30–R31 tutorial and contributor validation

## Scope and status

R01–R27 have independent acceptance.
R30 and R31 passed independent review with no findings.
The accepted report is preserved at `out/remediation/m6-r30-r31-accepted.md`.
The broader M6 gate remains open. This report does not accept R28–R29 or R32–R35 by implication.

## Independent review and snapshot amendment

Reviewer run `b2130268-bf19-4087-bdc1-86a8f9ea3944` accepted R30 and R31 only.
It reviewed the source, tests, and supplied command evidence without running commands or modifying files.
The verdict covers the 19-file `out/remediation/m6-r30-r31-review-manifest-format-update.json` against checkpoint `673d6c9d87c5aa69ce88306ca424c6788d9e3de2`.

The only source drift during review was six blank lines after the six `AGENTS.md` section headings, increasing the file from 3,635 to 3,641 bytes.
Removing exactly those six lines reproduced the original frozen SHA256.
The other 18 file hashes matched. The reviewer inspected this amendment before its final verdict.

The original manifest and review patch remain unchanged.
`m6-r30-r31-format-only.patch` and `m6-r30-r31-format-drift-evidence.json` preserve the exact amendment and parent hash verification.
The parent rechecked all 19 amended hashes, the empty index, and the protected release-script hash before recording acceptance.
This validation report changed only after that acceptance to record the result. Its old manifest hash remains historical evidence.

## R30: Executable concepts and integration

`docs/BASICS.md` now teaches facts, contracts, verbs, bindings, ordering, bundles, resolvers, compiler diagnostics, and outcomes.
`docs/INTEGRATION.md` separates embedded resolution from the daemon's supported HTTP boundary and explains storage ownership and pinned historical replay.
`docs/GETTING_STARTED.md` links the tutorial and uses the current sanitized conflict response.

The runnable tutorial is under `examples/embedded_orders/tutorial`.
Its `.eff` and `.effx` sources are embedded in the executable.
They create a string-valued review ticket and pass it to a second, void-returning operation.
A repeated execution preserves identity and does not repeat business operations.
A nonmatching input completes without operations.

The tutorial destination coordinates local business state and deduplication with one mutex.
It rejects conflicting argument/contract identity and recording without an earlier ticket.
Canceled work reports known-not-committed before mutation. An observed commit remains success.
This is an in-memory teaching implementation, not persistent destination deduplication or fencing enforcement.

The tutorial bundle contains explicit embedded descriptors for its registered resolver.
It is valid compiler input, but the daemon's HTTP-only resolver registry cannot execute it.
The guide states that boundary and does not advertise the checked IR output as daemon source-bundle input.

Tests cover:

- Both dialects, exact operation order, consumed result slots, and stable same-process replay
- Source-bundle and checked-IR round trips
- No matching plan
- Undeclared facts, wrong argument types, and use-before-binding
- CLI help, rejected syntax, and no successful output on compiler failure
- Concurrent destination duplicates, conflicting identity, and canceled/nonordered calls
- Equality between documented source snippets and the fixtures that compile and execute
- Execution of every documented tutorial-program command without passing its text to a shell

## Intentional example placement

The first layout introduced a fourth immediate example directory.
The inventory guard rejected it. Inspection confirmed an explicit budget of three immediate examples.
The walkthrough now extends the existing embedded onboarding path instead of adding a fourth catalog entry.
No inventory or budget changed. No new public product package, wire identity, or SQL migration was introduced.
The initial guard failure remains in `out/remediation/m6-r30-results.json` and its log.

## R31: Supported commands and regeneration

`AGENTS.md` and `CONTRIBUTING.md` now use existing Just recipes and direct Go, Buf, and SQLC commands.
They remove nonexistent coverage, test-database, Buf, and SQL recipes.
They distinguish the runtime SQLC input schema from `schema.MigrateSagaV2`, which the durable daemon uses.
The guides state which tools `just install` does not install.

`just test-examples` includes the tutorial tests.
It still only compiles the Go gRPC client, which currently has no live example test.
R33 remains open for authenticated Go/Python execution and TLS.

`just test-integration` now explicitly selects migration mode before tagged tests.
It requires an exported DSN, stops after migration failure, and passes credentials through quoted environment expansions rather than command interpolation.
An isolated command recorder executes the real recipe to test these properties, including shell metacharacters in the synthetic DSN.
The recorder test skips on Windows or if Just is unavailable. Just and a POSIX shell were available in this validation.
The recipe also ran against the existing task-owned PostgreSQL fixture.

A contributor-document test checks each referenced Just recipe against the actual recipe parser.
No new visible recipe or recipe-budget change was needed.

## R34 work required by these changes

The first integration run found a stale lexical test requiring the old `DB_DSN` alias spelling in `docs/INTEGRATION.md`.
It was not fixed by adding obsolete prose or removing startup validation.
Its replacement executes the documented prerequisite checks and passes the resulting arguments to a fresh process using the daemon's real flag definitions and mode validator.
It checks missing DSN, missing token, and valid configuration without opening a database, listener, or bundle.
The existing negative documentation fixtures remain intact.
This is partial R34 work, not completion of all documentation contracts.

## Validation evidence

Evidence is under `out/remediation/`:

- `m6-r30-results.json`: initial tutorial race tests, compiled-binary execution from an unrelated working directory, both dialects' bundle `check`/`compile`/`inspect`, expected diagnostic failures, and the initial inventory rejection.
- `m6-r30-r31-results.json`: tests after placement under embedded onboarding, repeated recipe race tests, `just test-examples`, and passing guardrails.
- `m6-r31-external-results.json`: `just build` passed with unchanged tracked generated files. The first integration run recorded the stale documentation-test failure.
- `m6-r30-r31-final-results.json`: repeated documentation contracts, successful PostgreSQL integration recipe, full repository race tests, and guardrails.

Primary LSP checks passed for the tutorial and new test files.
Generated Go protobuf and SQLC bindings were compared before and after `just build` and were byte-identical.
The PostgreSQL fixture is `effectus-remediation-c12270d5`, verified by its task ownership label before use.
No command targeted another database or deleted a fixture.

Later M6 work still needs the complete HTTP reference, live authenticated clients and TLS, remaining behavioral documentation gates, strict site build, and final independent review.
No production-readiness, destination exactly-once, or final R01–R40 completion claim follows from these results.
