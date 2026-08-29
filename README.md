# Effectus

![Effectus logo](./effectus-small.png)

**[Documentation](https://josephjohncox.github.io/effectus/)** · **[Getting started](https://josephjohncox.github.io/effectus/GETTING_STARTED/)** · **[Releases](https://github.com/josephjohncox/effectus/releases)**

Effectus is a typed rule compiler and execution runtime. It compiles `.eff` and `.effx` sources into checked protobuf IR.

The production daemon uses one durable execution engine for HTTP, Kafka, generated gRPC, and recovery work.

## What Effectus provides

- Static checks for fact paths, verb arguments, result bindings, and declared types
- Deterministic checked artifacts and content digests
- Immutable runtime generations with atomic activation
- Durable admission, execution, recovery, and saga state in PostgreSQL
- HTTP, Kafka, generated gRPC, CDC, SQL, S3, Iceberg, AMQP, Redis, and file adapters
- JSON and signed OCI extension bundles
- A status API, web UI, metrics, health probes, and a Helm chart

## Execution boundary

Effectus controls internal admission and execution state. It does not make an external service transactional.

External destinations must enforce the supplied idempotency key or fencing token. Compensation is recovery work, not an ACID rollback.

Read [Runtime Guarantees](docs/GUARANTEES.md) before you use Effectus in production.

## Install

Download prebuilt binaries, checksums, SBOMs, and signatures from the [latest release](https://github.com/josephjohncox/effectus/releases/latest).

Build from source when you need the examples or development tools:

```bash
git clone https://github.com/josephjohncox/effectus.git
cd effectus
git checkout v0.2.1
just build
export PATH="$PWD/bin:$PATH"
```

Follow the [checked walkthrough](https://josephjohncox.github.io/effectus/GETTING_STARTED/) to start PostgreSQL, compile a bundle, run `effectusd`, and submit facts.

## Compile rules

Define fact types and verb contracts, then write a rule:

```eff
rule "HighRiskLargeTxn" {
  when { transaction.amount > 1000 }
  then { FlagFraud(orderId: transaction.id) }
}
```

Create a bundle:

```bash
go run ./cmd/effectusc bundle \
  --name flow-ui-demo \
  --version 1.0.0 \
  --schema-dir examples/flow_ui_demo/schema \
  --verb-dir examples/flow_ui_demo/verbs \
  --verbschema examples/flow_ui_demo/schema/flow_verbs.json \
  --rules-dir examples/flow_ui_demo/rules \
  --output out/flow-ui-demo-bundle.json
```

The compiler checks each source file before it writes the bundle. Production generations contain checked first-order IR, not Go callbacks.

## Run the daemon

Effectusd requires PostgreSQL for durable workflow state:

```bash
EFFECTUS_API_TOKEN="replace-me" \
EFFECTUS_POSTGRES_DSN="postgres://effectus:password@db/effectus?sslmode=require" \
  effectusd --bundle out/flow-ui-demo-bundle.json --extensions-dir examples/flow_ui_demo/extensions --http-addr :8080
```

Open `http://localhost:8080/ui`. Use `/healthz` for liveness and `/readyz` for readiness.
To execute the repository's tested cold-start path, run `just ui-demo-smoke`.

Use environment variables or Kubernetes Secrets for credentials. Effectusd rejects secret command-line flags.

## Deploy an OCI bundle

Production OCI references must use a digest. The daemon also requires an operator-provided signature verifier.

```bash
EFFECTUS_API_TOKEN="replace-me" \
EFFECTUS_POSTGRES_DSN="postgres://effectus:password@db/effectus?sslmode=require" \
  effectusd \
  --oci-ref ghcr.io/acme/rules@sha256:BUNDLE_DIGEST \
  --oci-signature-verifier /usr/local/bin/effectus-verify-oci
```

Deploy a new digest to publish a new generation. Effectusd does not poll mutable OCI tags.

## Extend Effectus

Production effectusd supports checked HTTP, gRPC, stream, Kafka, and OCI-resolved executors. It rejects in-process Go plugins.

A trusted Go application can embed Effectus and register static executors. This compatibility path is outside the daemon isolation boundary.

Read [Extension System](docs/EXTENSION_SYSTEM.md) for manifest formats and security requirements.

## Documentation

Read the [published documentation](https://josephjohncox.github.io/effectus/) or browse the Markdown files in this repository.

- [Getting started](https://josephjohncox.github.io/effectus/GETTING_STARTED/)
- [Basics](docs/BASICS.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Runtime guarantees](docs/GUARANTEES.md)
- [Runtime lifecycle](docs/LIFECYCLE.md)
- [Runtime configuration](docs/RUNTIME_CONFIG.md)
- [Production runbook](docs/PRODUCTION_RUNBOOK.md)
- [Durable saga protocol](docs/DURABLE_SAGA_PROTOCOL.md)
- [CLI reference](docs/COMMANDS.md)
- [Examples](examples/README.md)

## Develop

```bash
just build
just test
just lint
```

Use `just --list` to show all repository tasks. See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution rules.

## License

Effectus uses the MIT license. See [LICENSE](LICENSE).
