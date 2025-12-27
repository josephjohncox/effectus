# Effectus - Typed, Deterministic Rule Engine

Effectus is a strongly-typed rule engine that turns live Facts into safe, deterministic Effects. It enforces types at compile time, supports dynamic extensions, and runs as a library or a daemon with hot-reloadable bundles.

## Highlights

- Typed facts and verbs with proto/JSON schema support
- Deterministic evaluation and static validation before runtime
- Dynamic extensions (JSON + OCI bundles) and static Go extensions
- Runtime daemon with UI, dry-run playground, ACLs, and rate limits
- Multi-source facts (Kafka, CDC, SQL, S3, Iceberg, AMQP, gRPC, Redis, files)

## Quick start

### 1) Install

```bash
go install github.com/effectus/effectus-go/cmd/effectusc@latest
go install github.com/effectus/effectus-go/cmd/effectusd@latest
```

### 2) Define facts (JSON schema or proto)

```json
// schemas/fraud_facts.json
[
  {"path": "transaction.id", "type": {"primitive": "string"}},
  {"path": "transaction.amount", "type": {"primitive": "float"}},
  {"path": "customer.tier", "type": {"primitive": "string"}}
]
```

### 3) Write rules

```eff
rule "HighRiskLargeTxn" {
  when { transaction.amount > 1000 }
  then { FlagFraud(orderId: transaction.id) }
}
```

### 4) Bundle + run

```bash
effectusc bundle \
  --name fraud-demo \
  --version 1.0.0 \
  --schema-dir schemas \
  --verb-dir verbs \
  --rules-dir rules \
  --output bundle.json

effectusd --bundle bundle.json --http-addr :8080
# open http://localhost:8080/ui
```

For non-library deployments (YAML config, ConfigMaps, mixed HTTP/OCI verbs), see `docs/RUNTIME_CONFIG.md`.

## Extensions (verbs)

Effectus supports multiple extension styles:

- Static: Go executors via `loader.NewStaticVerbLoader(...)`
- Dynamic: JSON verb manifests (`*.verbs.json`)
- OCI bundles: publish and hot-reload bundles from GHCR

Dynamic verb example (HTTP target):

```json
{
  "name": "ExternalAPI",
  "version": "1.0.0",
  "verbs": [
    {
      "name": "ValidateAccount",
      "argTypes": {"accountId": "string"},
      "requiredArgs": ["accountId"],
      "returnType": "ValidationResult",
      "target": {"type": "http", "config": {"url": "https://api.example.com/validate"}}
    }
  ]
}
```

See `docs/EXTENSION_SYSTEM.md` for full manifest schema and OCI publishing.

## Runtime UI + API

`effectusd` ships a lightweight status UI with rules, flows, schema summaries, a dependency graph, and a dry-run playground.
`/api/*` endpoints are token-protected by default; `/ui`, `/healthz`, and `/readyz` are open.

```bash
effectusd --bundle bundle.json --api-token devtoken
curl -X POST http://localhost:8080/api/facts \
  -H 'Authorization: Bearer devtoken' \
  -H 'Content-Type: application/json' \
  -d '{"universe":"prod","facts":{"customer":{"tier":"gold"}}}'
```

## Key docs

- `docs/README.md` - documentation index
- `docs/TUTORIALS.md` - short tutorials
- `docs/COMMANDS.md` - CLI reference
- `docs/RUNTIME_CONFIG.md` - non-library runtime config (YAML)
- `docs/FACT_SOURCES.md` - streaming and batch adapter tutorials
- `docs/EXTENSION_SYSTEM.md` - verbs and bundles
- `docs/SYSTEM_INTENT.md` / `docs/GLOSSARY.md` - model and vocabulary

## Development

```bash
just build
just test
just lint
```

See `CONTRIBUTING.md` for workflow details.

## License

MIT - see `LICENSE`.
