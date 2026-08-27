# Effectus - Typed, Deterministic Rule Engine

![Effectus logo](./effectus-small.png)

Effectus is a strongly-typed rule engine that turns live Facts into safe, deterministic Effects. It enforces types at compile time, supports dynamic extensions, and runs as a library or a daemon with hot-reloadable bundles.

## Highlights

- Typed facts and verbs with proto/JSON schema support
- Deterministic evaluation and static validation before runtime
- Dynamic extensions (JSON + OCI bundles) and static Go extensions
- Runtime daemon with UI, dry-run playground, ACLs, and rate limits
- Multi-source facts (Kafka, CDC, SQL, S3, Iceberg, AMQP, gRPC, Redis, files)

## Why Effectus

Effectus is a rules runtime built for correctness and change control. Compared to embedding rules in application code,
it keeps decision logic versioned, typed, and hot‑reloadable with canary checks and rollback. The runtime includes UI,
metrics, ACLs, and rate limits so the system can be monitored and governed in production.

## Good fit use cases

- Fraud/risk triage, policy enforcement, compliance gating
- Order routing, fulfillment orchestration, pricing eligibility
- Incident/SRE automation driven by live facts
- Access control/entitlements where auditability matters

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

## Standalone runtime (primary mode)

Run `effectusd` with a YAML/JSON config to operate as a standalone service:

```yaml
# effectusd.yaml
bundle:
  oci: "ghcr.io/myorg/bundles/fraud-demo@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

http:
  addr: ":8080"
metrics:
  addr: ":9090"

api:
  auth: "token"
  token: "replace-with-write-token"
  read_token: "replace-with-read-token"
  hotload_rules: true

facts:
  store: "file"
  path: "/var/lib/effectus/facts.json"
  merge_default: "last"
  cache:
    policy: "lru"
    max_universes: 200
    max_namespaces: 50

verbs:
  duplicate_policy: "error"
  oci_warmup: true
  strict: true

extensions:
  dirs: ["./extensions"]
  oci:
    - "ghcr.io/myorg/extension-bundles/payments@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

Run it:

```bash
EFFECTUS_API_TOKEN="..." \
EFFECTUS_SAGA_POSTGRES_DSN="postgres://effectus:...@db/effectus?sslmode=require" \
  effectusd --config effectusd.yaml \
  --oci-signature-verifier /usr/local/bin/effectus-verify-oci
```

Bundle/compiler example (creates a distributable bundle):

```bash
effectusc bundle \
  --name fraud-demo \
  --version 1.0.0 \
  --schema-dir examples/fraud_e2e/schema \
  --verb-dir examples/fraud_e2e/verbs \
  --rules-dir examples/fraud_e2e/rules \
  --output out/fraud-demo.bundle.json
```

For more config patterns (mixed verb sources, schema providers, Kubernetes ConfigMaps), see `docs/RUNTIME_CONFIG.md`.

## Extensions (verbs)

Effectus supports multiple extension styles:

- Static: Go executors via `loader.NewStaticVerbLoader(...)`
- Dynamic: JSON verb manifests (`*.verbs.json`)
- OCI bundles: publish digest-pinned, signed bundles and deploy a new digest for each release

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

### Go-backed verbs

Production effectusd rejects in-process Go plugins. Run Go-backed executors as separate HTTP or gRPC services and describe them with checked extension manifests.

A trusted embedded Go application can still register a static executor with `loader.NewStaticVerbLoader`. This library-only compatibility path is outside the daemon's process-isolation boundary.

## Runtime UI + API

`effectusd` ships a lightweight status UI with rules, flows, schema summaries, a dependency graph, and a dry-run playground.
`/api/*` endpoints are token-protected by default; `/ui`, `/healthz`, and `/readyz` are open.
Enable `/api/rules/validate` + `/api/rules/hotload` (and the in-UI rule editor) with `--rules-hotload` or `api.hotload_rules`.

```bash
EFFECTUS_API_TOKEN=devtoken \
EFFECTUS_SAGA_POSTGRES_DSN="postgres://effectus:...@db/effectus?sslmode=require" \
  effectusd --bundle bundle.json
curl -X POST http://localhost:8080/api/facts \
  -H 'Authorization: Bearer devtoken' \
  -H 'Content-Type: application/json' \
  -d '{"universe":"prod","facts":{"customer":{"tier":"gold"}}}'
```

## Library usage (embed in Go)

Use the library when you want in-process execution or custom wiring:

```go
ts := types.NewTypeSystem()
ts.RegisterFactType("order.id", types.NewStringType())
ts.RegisterFactType("order.total", types.NewFloatType())

registry := verb.NewRegistry(ts)
_ = registry.RegisterVerb(&verb.Spec{
  Name:       "FlagHighValue",
  ArgTypes:   map[string]string{"orderId": "string"},
  ReturnType: "bool",
  Executor:   myExecutor,
})

facts := common.NewBasicFacts(map[string]interface{}{
  "order": map[string]interface{}{"id": "o-1", "total": 2500.0},
}, ts)

comp := compiler.NewCompiler()
compTS := comp.GetTypeSystem()
compTS.RegisterFactType("order.id", types.NewStringType())
compTS.RegisterFactType("order.total", types.NewFloatType())
compTS.RegisterVerbType("FlagHighValue", map[string]*types.Type{"orderId": types.NewStringType()}, types.NewBoolType())

parsed, _ := comp.ParseAndTypeCheck("rules/flags.eff", facts)
specAny, _ := (&list.Compiler{}).CompileParsedFile(parsed, "rules/flags.eff", facts.Schema())
spec := specAny.(*list.Spec)
spec.VerbRegistry = registry
_ = spec.Execute(context.Background(), facts, nil)
```

See `docs/TUTORIALS.md` for a compact library walkthrough and extension loaders.

## UI demos

Run the demo bundles + UI locally:

```bash
just ui-demo
just ui-flow-demo
```

## Screenshots & video

![Flow UI demo placeholder](docs/media/ui-flow-demo.gif)

## Key docs

- `docs/README.md` - documentation index
- `docs/TUTORIALS.md` - short tutorials
- `docs/COMMANDS.md` - CLI reference
- `docs/RUNTIME_CONFIG.md` - non-library runtime config (YAML)
- `docs/PRODUCTION_RUNBOOK.md` - production checklist, hotload, rollback
- `docs/FACT_SOURCES.md` - streaming and batch adapter tutorials
- `docs/EXTENSION_SYSTEM.md` - verbs and bundles
- `docs/SYSTEM_INTENT.md` / `docs/GLOSSARY.md` - model and vocabulary

## Production & Helm

- Helm chart: `charts/effectusd/` (OCI-ready runtime chart)
- Runtime config (non-library mode): `docs/RUNTIME_CONFIG.md`
- Production runbook: `docs/PRODUCTION_RUNBOOK.md`

## Development

```bash
just build
just test
just lint
```

See `CONTRIBUTING.md` for workflow details.

## License

MIT - see `LICENSE`.
