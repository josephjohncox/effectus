# Getting Started

This walkthrough runs the checked HTTP path. It uses the fraud example and a local PostgreSQL database.

## Requirements

Install these tools before you start:

- Git
- Go 1.25.13 or a compatible Go 1.25 toolchain
- Docker with Docker Compose
- [`just`](https://github.com/casey/just)
- `curl`

## 1. Get the source

Clone the repository and select the current release:

```bash
git clone https://github.com/josephjohncox/effectus.git
cd effectus
git checkout v0.2.1
```

The [GitHub release](https://github.com/josephjohncox/effectus/releases/tag/v0.2.1) also provides prebuilt binaries, checksums, SBOMs, and signatures.

## 2. Run the automated walkthrough

Run the tested cold-start path:

```bash
just ui-demo-smoke
```

This command performs these actions:

1. Starts PostgreSQL with Docker Compose.
2. Compiles the example into checked protobuf IR.
3. Applies the database migrations.
4. Starts `effectusd` with the checked bundle.
5. Submits facts through the authenticated HTTP API.
6. Confirms that an idempotent replay returns the same execution ID.
7. Confirms that a conflicting replay returns HTTP 409.
8. Stops the daemon and removes the test database.

A successful run ends with this message:

```text
OK checked UI cold-start and replay smoke passed
```

## 3. Start the demo database

Run the next steps to keep the daemon open for inspection.

```bash
just setup-db
```

The local database listens on port `55433`. The example uses credentials for local development only.

## 4. Build the checked bundle

Create the output directory:

```bash
mkdir -p out/ui_demo
```

Compile the example:

```bash
go run ./cmd/effectusc bundle \
  --name fraud-ui-demo \
  --version 1.0.0 \
  --schema-dir examples/fraud_e2e/schema \
  --verb-dir examples/fraud_e2e/verbs \
  --verbschema examples/fraud_e2e/schema/fraud_verbs.json \
  --rules-dir examples/fraud_e2e/rules \
  --output out/ui_demo/bundle.json
```

The compiler checks fact paths, verb arguments, bindings, and declared types. It writes the bundle only after all checks pass.

## 5. Apply the migrations

Set the local database connection:

```bash
export EFFECTUS_POSTGRES_DSN='postgres://effectus:effectus@localhost:55433/effectus_saga?sslmode=disable'
```

Apply the runtime migrations:

```bash
go run ./cmd/effectusd --database-migrations=apply
```

Production deployments must apply migrations with a controlled deployment job. Do not let multiple daemon instances race to apply them.

## 6. Start the daemon

Run this command in the first terminal:

```bash
EFFECTUS_API_TOKEN='demo-token' \
EFFECTUS_POSTGRES_DSN="$EFFECTUS_POSTGRES_DSN" \
  go run ./cmd/effectusd \
  --bundle out/ui_demo/bundle.json \
  --extensions-dir examples/fraud_e2e/extensions \
  --facts-store memory \
  --http-addr 127.0.0.1:8080 \
  --metrics-addr ''
```

Wait until the readiness endpoint returns HTTP 200:

```bash
curl --fail http://127.0.0.1:8080/readyz
```

Open [http://127.0.0.1:8080/ui](http://127.0.0.1:8080/ui) to inspect the bundle, rules, graph, verbs, and facts.

## 7. Submit facts

Run this command in a second terminal:

```bash
curl --fail-with-body --silent \
  --request POST http://127.0.0.1:8080/api/facts \
  --header 'Authorization: Bearer demo-token' \
  --header 'Idempotency-Key: walkthrough-001' \
  --header 'Content-Type: application/json' \
  --data @examples/fraud_e2e/data/facts_payload.json
```

The API returns HTTP 202 with an execution ID and a generation digest:

```json
{
  "execution_id": "...",
  "generation_digest": "...",
  "status": "accepted"
}
```

Submit the same request again. Effectus returns the same execution ID for the same idempotency key and payload.

Change the payload but keep the idempotency key. Effectus rejects the conflicting replay with HTTP 409.

## 8. Inspect the runtime

Check the global runtime status:

```bash
curl --fail --silent \
  --header 'Authorization: Bearer demo-token' \
  http://127.0.0.1:8080/api/status
```

Read the stored local fact projection:

```bash
curl --fail --silent \
  --header 'Authorization: Bearer demo-token' \
  'http://127.0.0.1:8080/api/facts?universe=demo'
```

The local fact store is an inspection projection. PostgreSQL stores the durable execution and recovery state.

## 9. Stop the demo

Stop `effectusd` with `Ctrl+C`.

Remove the local database containers and volumes:

```bash
COMPOSE_PROJECT_NAME=effectus-ui-demo-smoke \
  docker compose -f examples/saga_stack/docker-compose.yml down -v
```

## Next steps

- Read [Effectus Basics](BASICS.md) to learn the rule model.
- Read the [CLI Reference](COMMANDS.md) to build and inspect your rules.
- Read [Runtime Configuration](RUNTIME_CONFIG.md) before you configure `effectusd`.
- Read [Runtime Guarantees](GUARANTEES.md) before a production deployment.
- Read the [Production Runbook](PRODUCTION_RUNBOOK.md) before operations work.
