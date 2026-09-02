# Getting Started

Effectus has two first-run paths. Both paths use the same order-review rule and scenario artifact.

| Path | Use it when | State |
| --- | --- | --- |
| Embedded Go | A Go service runs rules in its own process | Process-local and ephemeral |
| Durable Docker | `effectusd` and a business executor run as services | PostgreSQL-backed and restart-safe |

Both paths use these values:

- Namespace: `merchant-42`
- Order ID: `order-200`
- Total: `2499.00`
- Currency: `USD`
- Risk score: `82`
- Rule: `ReviewLargeOrder`
- Verb: `RequestManualReview`
- Reason: `value_or_risk`
- Idempotency key: `order-200-created`

Get the current source before you select a path:

```bash
git clone https://github.com/josephjohncox/effectus.git
cd effectus
git checkout main
```

Use a release tag instead of `main` for production evaluation. The commands on this page track the current branch.

## Path 1: Embedded Go

This path requires only Go 1.25 or later after you get the source. It does not require Docker, Buf, or Python.

Run the checked embedded application:

```bash
go -C examples run ./embedded_orders
```

The command writes a compile log to standard error. The line ends with `Runtime compiled successfully with 1 verbs, 0 functions`.

The command writes JSON to standard output. The admission identity deterministically generates the execution ID, so the same namespace, idempotency key, ruleset, and version produce the same ID in later process runs. The default ledger and outbox remain process-local: replay records and workflow state do not survive a process exit.

```json
{
  "completed": true,
  "execution_id": "...",
  "replayed_execution": "...",
  "review_count": 1
}
```

`execution_id` and `replayed_execution` must match. `review_count` must be `1`.

The application compiles `examples/order_review/rules/order_review.eff` to checked IR. It uses a process-local ledger and creates no persistent state.

## Path 2: Durable Docker

This path uses Docker Compose for all services. The script creates the immutable source bundle, then builds and starts `effectusd` from the current checkout.

Install these host tools before you run the acceptance script:

- Bash
- Docker with the Compose plugin
- `curl`
- Python 3
- Go 1.25 or later

Buf is not required. The script checks every prerequisite and checks the Docker daemon before it creates resources.

Run the durable path:

```bash
export EFFECTUS_API_TOKEN='local-api-token'
export EXECUTOR_TOKEN='local-executor-token'
export EFFECTUS_DEMO_HTTP_PORT=18080
export EXECUTOR_DEMO_HTTP_PORT=8090
examples/standalone_executor/scripts/run.sh
```

The script performs these checks:

1. It creates `out/standalone_executor`.
2. It creates a `bundle.SourceBundle` from the shared rule and executor descriptor.
3. It includes the HTTP executor descriptors in the source bundle.
4. It builds the current `effectusd` image.
5. It starts PostgreSQL at `postgres:5432`.
6. It applies migrations before `effectusd` starts.
7. It submits the shared order request.
8. It restarts `effectusd` and the business executor.
9. It replays the request and checks the execution ID.
10. It checks that PostgreSQL contains one business review.
11. It submits a conflicting replay and requires HTTP 409.

A successful run ends with output in this form:

```text
{
  "execution_id": "...",
  "replay_ids_match": true,
  "replayed_execution_id": "...",
  "review_count": 1
}
conflicting_replay_http_status: 409
{
  "error": "engine admission identity conflict: admission identity ..."
}
OK durable order-review demo passed
```

Compose and image-build progress can appear on standard error. The script prints Compose logs after a startup or readiness failure.

The services stay active after a successful run. Use the authenticated `/v1/status` endpoint to inspect the active generation.

The host ports bind only to the loopback interface:

| Service | Host address | Container address |
| --- | --- | --- |
| `effectusd` | `127.0.0.1:18080` | `effectusd:8080` |
| Business executor | `127.0.0.1:8090` | `business-executor:8090` |
| PostgreSQL | Not published | `postgres:5432` |

Override the HTTP ports when the defaults are not available:

```bash
export EFFECTUS_DEMO_HTTP_PORT=28080
export EXECUTOR_DEMO_HTTP_PORT=28090
export EFFECTUS_API_TOKEN='local-api-token'
export EXECUTOR_TOKEN='local-executor-token'
examples/standalone_executor/scripts/run.sh
```

The inspection commands below use these exported variables.

## Inspect the durable path

Check readiness:

```bash
curl --fail --silent \
  --header "Authorization: Bearer ${EFFECTUS_API_TOKEN}" \
  "http://127.0.0.1:${EFFECTUS_DEMO_HTTP_PORT}/v1/status"
```

Read the one business review:

```bash
curl --fail --silent \
  --header "X-Demo-Token: ${EXECUTOR_TOKEN}" \
  "http://127.0.0.1:${EXECUTOR_DEMO_HTTP_PORT}/reviews" |
  python3 -m json.tool
```

Read service logs:

```bash
docker compose \
  -f examples/standalone_executor/docker-compose.yml \
  logs --no-color --tail=200
```

## Stop the durable path

The run script refuses to start when the selected Compose project already has containers, networks, or volumes. It does not delete those resources. The cleanup command is the explicit reset action: it deletes the selected project's containers, network, and PostgreSQL volume.

**Warning:** This command permanently deletes all execution and review data from this demo.

```bash
examples/standalone_executor/scripts/down.sh
```

You can run the cleanup command more than once.

## Troubleshooting

### A required command is missing

Install the command named in the error. Run the script again after the command is available on `PATH`.

The onboarding paths do not use Buf. Documentation generation and protobuf development tasks can have additional requirements.

### The Docker daemon is not available

Start Docker Desktop or the Docker service. Confirm that `docker info` succeeds before you run the script again.

### Docker Compose is not available

Install the Docker Compose plugin. Confirm that `docker compose version` succeeds.

The script does not support the old `docker-compose` command.

### The Compose project already has resources

The script preserves existing containers, networks, and volumes. Inspect the existing project, or run the cleanup command when you intend to delete its data. Use the same `EFFECTUS_DEMO_PROJECT` value for the run and cleanup commands.

### A host port is not available

Stop the process that uses the port. You can also set `EFFECTUS_DEMO_HTTP_PORT` and `EXECUTOR_DEMO_HTTP_PORT`.

The script rejects equal ports and values outside the range `1` through `65535`.

### The bundle file is missing

Run the complete script from the repository checkout. The script creates `out/standalone_executor` before it starts Compose.

Do not run `docker compose up` before the bundle exists.

### PostgreSQL is not reachable from a container

Use `postgres:5432` inside this Compose project. Do not use `localhost` or a host-published PostgreSQL port.

The `migrate` service must finish successfully before `effectusd` starts. Inspect its log with this command:

```bash
docker compose \
  -f examples/standalone_executor/docker-compose.yml \
  logs --no-color migrate postgres
```

### A service does not become ready

Read the logs that the script prints. Fix the first migration, bundle, token, or port error.

The script removes only the failed stack that the current invocation created. Run the cleanup script if the host stopped during cleanup.

### A replay returns a different execution ID

Make sure the namespace, idempotency key, rule version, and request body did not change.

A matching replay returns the original execution ID. A changed request with the same identity returns HTTP 409.

### Cleanup did not remove the demo

Run the cleanup command with the same `EFFECTUS_DEMO_PROJECT` value that you used to start the stack.

```bash
EFFECTUS_DEMO_PROJECT=standalone_executor \
  examples/standalone_executor/scripts/down.sh
```

## Replay identity across paths

Compare replay IDs only within one path. The embedded path and the durable path use separate ledgers.

The two paths do not need to produce the same execution ID.

## Next steps

- Read [Effectus Basics](BASICS.md) for the language model.
- Read [Integration Guide](INTEGRATION.md) before you select a production boundary.
- Read [Runtime Guarantees](GUARANTEES.md) before a production deployment.
- Read [Production Runbook](PRODUCTION_RUNBOOK.md) before operations work.
