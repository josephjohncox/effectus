# Standalone Compose first-run and restart validation

**Independently accepted for the bounded local Compose scope.**

This is a separately authorized follow-up to the [R39/R40 external gates](m7-final-review.md#residual-limits-and-unexecuted-gates).
It does not replace the accepted remediation evidence or authorize the other external gates.
The parent created one isolated stack and used a retention-safe harness.
The checked-in `run.sh` and `down.sh` were not executed.

## Source and setup

The project is `effectus-compose-gate-d4c693eb5f87`.
The local Docker client and server reported 29.2.1 on Linux arm64.
Docker Compose reported 5.0.2. The local BuildKit worker reported v0.27.1.
The parent used the inspected local Docker endpoint and its default Docker-backed builder.

Both application images used the unchanged, digest-pinned Dockerfiles.
Those files select Go 1.25.13-alpine builders and scratch runtimes.
PostgreSQL used the unchanged, digest-pinned 16-alpine image.
The parent recorded the resulting image IDs and checked them against each container.
This is not a bit-for-bit reproducibility or remote CI result.

The build inventory contains 374 current tracked or nonignored files, including uncommitted remediation.
The source-only context contains 362 members after the applicable Docker-ignore exclusions.
It excludes ignored local tools, caches, credentials, and agent state.
The original Docker ignore file does not exclude all those local directories.
The parent therefore did not send the whole working directory as a build context.
No application source, Dockerfile, example script, or checked-in Compose configuration changed.

The harness rendered the checked-in Compose model, then changed these fixture settings:

- A unique project, network, volume, application image tags, and ownership labels.
- Two selected loopback-only host ports. PostgreSQL has no host port.
- Generated API, executor, and database credentials.
- An owner-only bundle path outside the example's normal output path.
- A bind-mount check that rejects a missing bundle source.
- A 512 MiB memory limit and two-CPU limit per service.

The model retains its services, dependencies, migration command, and entrypoints.
The parent checked resource-name absence and image IDs before creation.
It checked ownership, identities, mount paths, ports, and image IDs before each restart.
The generated configuration and credential state remain owner-only local files.
Docker administrators can inspect container environments. This is not a secret-isolated host.

## Executed checks

The initial stack had one new PostgreSQL volume.
The migration service exited successfully before the daemon started.
The daemon status endpoint and executor health endpoint became available.
Requests without credentials received HTTP 401 from both protected endpoints.

The parent submitted the shared order-review scenario through authenticated HTTP.
It waited for terminal completion, then saved the executor's review view and database observations.
Each relation below started empty and contained exactly one row after completion:

| Relation | Before execution | After execution |
| --- | ---: | ---: |
| `effectus_executions` | 0 | 1 |
| `effectus_execution_plans` | 0 | 1 |
| `effectus_saga_instances` | 0 | 1 |
| `effectus_saga_steps` | 0 | 1 |
| `effectus_saga_outbox` | 0 | 1 |
| `effectus_saga_attempts` | 0 | 1 |
| `order_reviews` | 0 | 1 |

The following checks passed:

| Restart scope | Method | Replay result |
| --- | --- | --- |
| Daemon only | Stop and start the same daemon container | Same completed execution and review |
| Executor only | Stop and start the same executor container | Same completed execution and review |
| All long-running services | Stop applications before PostgreSQL. Start PostgreSQL before the applications | Same completed execution and review |

The last scope includes the daemon, executor, and PostgreSQL.
The completed one-shot migration container remained present. The harness did not rerun migration.
Every stopped service exited with code zero. No OOM termination was observed.
Started-at values changed for the intended services. Container IDs, image IDs, network identity, and volume identity remained unchanged.

Every replay returned HTTP 202 with the original execution ID and `completed: true`.
The complete review view remained unchanged, including its identity, argument hash, status, and timestamp.
Captured relation counts and selected durable row fields also remained unchanged.
The attempt count stayed at one. No new dispatch attempt was recorded.

The parent then changed the order's risk score under the same idempotency key.
The request returned HTTP 409. A subsequent valid replay preserved the saved review and database observations.
These checks did not directly redeliver an invocation to the executor or inject a lost commit response.
They do not establish every destination deduplication or unknown-outcome recovery path.

## Evidence and retained state

The task-local harness is in:

- `out/remediation/compose-gate.py`
- `out/remediation/compose-gate-validate.py`

Completed stages do not repeat operations. An interrupted or failed operation requires inspection before a deliberate correction or retry.
These are retained audit harnesses, not new supported CLI entrypoints or cleanup tools.

`out/remediation/compose-gate-results.json` summarizes 134 command records and 24 HTTP records.
One command failed before compilation. The successful retry and all later execution records remain separate.
Per-command receipts retain exit status and log hashes. HTTP receipts omit authorization headers.
The source inventory and archive-envelope correction have separate records.

The parent checked all 374 inventoried source files before the documentation amendments. No source drift occurred during execution.
It also scanned 312 public artifacts for the three generated secret values and found no matches.
That scan does not cover arbitrary encodings or the whole repository.

All six earlier PostgreSQL/Kafka fixtures remained running.
Their IDs, images, labels, ports, and started-at values matched the saved pre-run observations.
The new stack also remains retained: three running services and one successful, exited migration container.
Its network, PostgreSQL volume, two application images, fixture rows, and private configuration remain available for inspection.
No removal, pruning, commit, push, registry publication, or production deployment occurred.
Cleanup still requires separate authorization and fresh ownership checks.

## Preserved preparation failures

The first read-only helper stopped on two incorrect paths supplied by the parent.
The parent corrected those paths from the Compose file and retried the same read-only preparation.
No service or source change resulted from that failed lookup.

The first Docker build rejected the uncompressed stdin archive:

```text
ERROR: failed to build: ambiguous Dockerfile source: both stdin and flag correspond to Dockerfiles
```

The archive contained both Dockerfiles. Docker's detector examines only a 1,024-byte prefix.
The initial PAX extended header required another header beyond that prefix.
The parent compressed the same tar bytes with gzip and verified byte-identical decompression and all 362 members.
Docker recognized that envelope, and both builds passed without source changes.
The original archive, failed build receipt, original harness, and correction receipt remain preserved.

A canceled turn did not erase completed preparation or the recorded failure.
The parent resumed saved work rather than recreating the fixture.
Primary Python checks passed after the parent corrected an optional-member guard and explicit HTTP response initialization.
Those harness checks are not application test execution or universal analyzer acceptance.

## Limits

- One shared scenario, one committed execution, and three controlled restart scopes.
- Source-only local builds, not the literal `run.sh` path or an unfiltered working-directory build.
- Same-container stop/start with a retained volume, not removal/recreation, power loss, host reboot, or storage corruption.
- Authenticated HTTP intake, not Kafka intake. The combined Kafka-to-business-commit gate remains open.
- Selected database fields and row counts, not byte-level comparison of every stored column or packet-level invocation tracing.
- Local, bounded resources and plaintext fixture transports, not production capacity, deployed Kubernetes, registry, or recovery certification.
- Earlier coverage, benchmarks, clock tests, broker tests, and Go suites retain their original measurement chronology. They were not repeated here.

## Independent acceptance

Reviewer `3636247b-6797-469b-af3b-f02099e5923a` accepted the bounded first-run/restart gate and evidence reconciliation with no findings.
The merge verdict is **OK for this local Compose follow-up only**.
The preserved report is `out/remediation/compose-gate-accepted.md`.
This was independent source/evidence assessment, not independent execution or service inspection.

The accepted manifest is `out/remediation/compose-gate-review-manifest.json`.
Its SHA256 is `44c5b1ce55d18f9607028cec43ecacdf7bd022a602da1d136db377c2248724b5`.
The parent verified all seven scope files, twelve references, nineteen archive members, and 322 public evidence files before status amendments.
`compose-gate-acceptance-verification.json` records that verification and byte-identical report preservation.
The earlier 312-artifact secret scan remains a separate execution-stage observation.

The accepted archive retains its pending-status text.
Later acceptance-only amendments and documentation checks have separate records. They do not repeat the stack tests.
Only the Compose gate closes. R01–R40 remain accepted, and the other four external gates remain open.
Approval does not authorize publication, deployment, or cleanup.
