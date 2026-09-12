# Kafka to business commit validation

**Independently accepted for the bounded local Kafka-to-business-commit scope.**

This follow-up tests one Kafka record through the real daemon, PostgreSQL ledger/outbox, and PostgreSQL-backed business executor.
It extends the accepted [standalone Compose gate](standalone-compose-validation.md). It does not reopen R01–R40.
The user authorized a new isolated fixture after the parent requested permission for this gate and deduplication checks.

## Fixture and source boundary

The project is `effectus-kafka-gate-1151dfac978e`.
It contains PostgreSQL, Redpanda, the business executor, a response-loss proxy, a migration container, and `effectusd`.
Only task-owned resources received writes or restarts.
All ten earlier containers matched their before/after identity, image, state, start-time, label, port, mount, and network observations.

The fixture reuses the accepted standalone engine and executor images without retagging or rebuilding them.
It also reuses the previously verified PostgreSQL 16 and Redpanda v24.2.10 image digests.
All application/build inputs match the reused images' source inventory. Later documentation differs, so this is not an exact-final-tree image claim.
The parent verified all 375 current inventoried source files unchanged throughout execution, before the report amendments.

Three task-only Go helpers provide the fault proxy, Kafka producer/observations, and bundle generation.
Their sources are under `out/remediation/.kafka-gate-src/`.
They add no module, dependency, public API, example, or recipe.
The parent built native, CGO-disabled binaries and recorded source/binary hashes.
The proxy binary is a read-only bind mount into the unchanged engine image, with its entrypoint overridden.

The task bundle uses the existing rule and checked retry contract.
Only the executor destination changes to the proxy, and its timeout changes from 5s to 60s.
The contract retains sink-guaranteed idempotency, three attempts, and the existing backoff bounds.
The PostgreSQL-backed executor remains unchanged.

The real daemon uses these flags:

```text
--fact-source=kafka
--kafka-brokers=broker:9092
--kafka-topic=effectus-kafka-gate-1151dfac978e-facts
--kafka-consumer-group=effectus-kafka-gate-1151dfac978e-consumer
--kafka-ack-contract=completed_processing
```

The fixture has one broker, one topic partition, and replication factor one.
Kafka has separate internal and loopback-advertised listeners.
All published ports bind to `127.0.0.1`. PostgreSQL has no host port.
New credentials and configuration remain in owner-only files and directories.

## Initial offset and message

The daemon defaults to the latest start offset when no group commit exists.
The harness therefore creates an empty topic and seeds the new group's next offset to 0 before daemon startup.
It uses `rpk group seek --to start --topics … --allow-new-topics` for this task-owned group only.
This fixture setup avoids a producer/consumer startup race. It does not test the daemon's unconfigured latest-offset behavior.

The harness checks that the group is stable with one member assigned partition 0 before publication.
The producer then sends one record with `RequireAll` acknowledgments.
Its JSON contains the shared order-review request, with a task-specific namespace and universe.
There is no HTTP admission request in this end-to-end flow.

The durable admission identity is `kafka/default/<topic>/0/0`.
The message key does not supply the Effectus idempotency key.
A new record at another offset would have a different delivery identity, even with the same body. That case is not tested here.

## Business commit before Kafka acknowledgment

The proxy forwards the first invocation unchanged to the real executor.
It reads the executor's HTTP 200 response and review ID, then holds the client-facing response.
The parent observes PostgreSQL and Kafka while that response remains held.

| Observation | While response held | After retry and completion |
| --- | --- | --- |
| Business reviews | 1 | 1, same captured row |
| Execution state | `running` | `completed`, same ID |
| Dispatch state | `in_flight`, attempt 1 | `succeeded`, attempt 2 |
| Attempt rows | 1 | 2 |
| Attempt outcomes | First attempt unfinished | `unknown_outcome`, then `success` |
| Kafka next committed offset | 0 | 1 |

All seven selected business/journal relations start empty.
Each contains one row during the held-response observation.
After completion, only the attempt relation has two rows. The other six still have one.

The parent then releases the proxy barrier.
The proxy closes the client connection without a response, after the destination business commit.
The checked runtime records an unknown outcome and retries the same dispatch.
The executor handles two physical requests with identical execution, saga, effect, idempotency, argument-hash, and contract-hash identities.
Only the attempt number changes from 1 to 2.
Both backend responses identify the same review. The captured business row remains identical, including its creation time.
There is no cancellation call or new business review.

This observes destination deduplication after an actual lost response in the combined flow.
The source also shows that the executor returns success only after its transaction commits.
It does not rely solely on a declared idempotency policy or process-local counter.

## Forced Kafka redelivery

After the first offset commit, the harness stops the daemon and waits for an empty consumer group.
It rewinds only this group's topic offset from 1 to 0.
It restarts the same daemon container and observes the next offset return to 1.

The execution identity, selected durable rows/counts, and proxy event journal remain unchanged.
No third executor call occurs. The completed execution handles the repeated delivery without another business invocation.
This is an intentional same-record replay, not merely a restart that finds no uncommitted data.

## Full retained-stack restart

The harness stops the daemon, proxy, executor, broker, and PostgreSQL in dependency order.
Every stopped service exits with code 0 and without OOM termination.
It starts the same containers, waits for dependencies and group assignment, then compares the observations again.

Container identities, the network identity, and both named volume identities remain unchanged.
The five service start times change. The successful migration container's start time does not change, because migration is not rerun.
The PostgreSQL rows, Kafka next offset 1, and persisted proxy event journal remain unchanged.

## Evidence and checks

The parent result summary is `out/remediation/kafka-gate-results.json`.
It records 135 command operations and 20 HTTP operations.
One broker-readiness probe returned 1 during restart, followed by a successful probe.
That expected not-ready observation remains recorded. It is not an omitted failure or a claim that every command exited zero.

The proxy unit test passed three race-enabled runs before service startup.
After live execution, the parent formatted only that test, preserved its original bytes, and passed three further race-enabled runs.
The current test matches the original's canonical `gofmt` output. All running binaries remain unchanged.
The parent also scanned 317 public gate/source files for the three generated credential literals, with no matches.
That was a bounded literal scan, not a general secret audit.
`kafka-gate-public-scan.json` records it separately from any later review-packet inventory.

Key evidence under `out/remediation/`:

- `kafka-gate-held-business-commit-database.json`: committed business row while execution and dispatch remain active.
- `kafka-gate-while-business-committed-offset.log`: next committed offset 0 during that window.
- `kafka-gate-committed-database.json`: completed execution, one review, and two attempt outcomes.
- `kafka-gate-committed-proxy.json`: two backend successes, one dropped response, stable metadata, and the same review ID.
- `kafka-gate-rewound-offset.log` and `kafka-gate-after-replay-offset.log`: deliberate rewind and recommit.
- `kafka-gate-after-replay-database.json` and `kafka-gate-after-restart-database.json`: unchanged captured durable state.
- `kafka-gate-restart-before-ownership.json` and `kafka-gate-restart-after-ownership.json`: same resource identities and intended start-time changes.
- `kafka-gate-existing-fixtures-before.json` and `kafka-gate-existing-fixtures-after.log`: unchanged ten-container baseline.

The two Python harnesses journal operations and refuse repeated command labels, including interrupted or failed labels.
No build, publication, offset rewind, response loss, or restart stage was repeated after interruption.
A canceled silent turn had completed source/protocol inspection only. The parent resumed that work before creating this fixture.

## Retention and limits

All six new containers remain retained: five running services and the successful exited migration container.
Both named data volumes, the network, proxy journal, helper binaries, configuration, messages, and rows remain retained.
The earlier ten containers remain unchanged. No images were retagged or removed.
No commit, push, registry publication, production deployment, or destructive cleanup occurred.

The comparisons use selected SQL columns and seven named relations, not a byte-for-byte database backup.
The fixture observes one successful execution, one dropped response, one forced record replay, and one controlled full-stack restart.
It does not test terminal-failure acknowledgment policy, duplicate bodies at new offsets, broker replication/failover, host loss, or adversarial destinations.
It uses local plaintext Kafka/HTTP and synthetic data. It is not an authentication, production recovery, throughput, or universal exactly-once certification.
Earlier full-root and integration Go suites were not rerun.
Coverage, benchmarks, and model checks keep their original chronology.

## Independent acceptance

Reviewer `45b78f34-0e94-48f3-abf5-c02a81f5fb5a` accepted the combined gate and evidence reconciliation with no findings.
The merge verdict is **OK for this bounded local Kafka follow-up only**.
The preserved report is `out/remediation/kafka-gate-accepted.md`.
This was independent source/evidence assessment, not independent execution or service inspection.

The accepted manifest is `out/remediation/kafka-gate-review-manifest.json`.
Its SHA256 is `269052e55c2561fae1e7521b6b75b1e98c4f9410e6ab9ebec4ec0d65843ab2c1`.
The parent verified all eleven scope files, twenty references, thirty-one archive members, and 327 evidence files before status amendments.
`kafka-gate-acceptance-verification.json` records that verification and byte-identical report preservation.
The earlier 317-file credential scan remains a separate execution-stage observation.

The accepted archive retains its pending-status text.
Later acceptance-only amendments and documentation checks have separate records. They do not repeat the live fixture tests.
Only the combined Kafka gate closes. R01–R40 and standalone Compose remain accepted.
The [other three external gates](m7-final-review.md#residual-limits-and-unexecuted-gates) remain open.
Approval does not authorize publication, production deployment, or cleanup.
