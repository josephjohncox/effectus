| Area                |  **Effectus** naming               | Purpose (one-liner)                                    |
| ------------------- |  --------------------------------- | ------------------------------------------------------ |
| Compiler CLI        |  **`effectusc`**                   | Parse + type-check rules, emit IR/JSON, fail on errors |
| Linter              |  **`effectus lint`**               | Stylistic checks, dead predicates, unreachable rules   |
| Executor daemon     |  **`effectusd`**                   | gRPC/HTTP service that ingests Facts ⟶ Effects         |
| Local runner        |  **`effectus run`**                | CLI harness for dev / CI, pipes JSON Facts ⟶ stdout    |
| Hot-reload side-car |  **`effectus-watch`**              | Watches rule directory, sends SIGHUP to `effectusd`    |
| VS Code ext         |  **`effectus-vscode`**             | Syntax highlight + autocompletion from schema registry |
| Go SDK module       |  `github.com/yourorg/effectus-go`  | Pure library: parser, compiler, executor stubs         |
| Docker image        |  `ghcr.io/yourorg/effectusd:<ver>` | Container for prod deploys                             |
| Env var prefix      |  **`EFFECTUS_…`**                  | e.g. `EFFECTUS_RULE_PATH=/etc/effectus`                |

### Quick examples

```bash
# 1. Compile & lint
effectusc compile rules/base.eff          # exit 0 or 1
effectus lint    rules/base.eff --fix     # auto-format

# 2. Run locally
cat facts/batch123.json | effectus run rules/  \
    --executor=log | jq .

# 3. Prod daemon (hot reload)
docker run -d \
  -v /etc/effectus:/rules \
  -e EFFECTUS_RULE_PATH=/rules \
  ghcr.io/yourorg/effectusd:v0.8.2
effectus-watch /rules --signal --pid=$(pgrep effectusd)
```

# Effectus Commands

Effectus comes with several command-line tools for working with rule files and bundles.

## effectusc

The `effectusc` command is the main compiler and utility for working with Effectus rules. It provides several subcommands for different operations.

### parse

Parses rule files without type checking them:

```bash
effectusc parse [options] file1.eff [file2.eff ...]

Options:
  --verbose      Show detailed output
```

### typecheck

Parses and type checks rule files against schemas:

```bash
effectusc typecheck [options] file1.eff [file2.eff ...]

Options:
  --schema       Comma-separated list of schema files to load
  --verbschema   Comma-separated list of verb schema files to load
  --output       Output file for reports (defaults to stdout)
  --report       Generate type report
  --verbose      Show detailed output
```

### compile

Compiles rule files into a unified specification:

```bash
effectusc compile [options] file1.eff [file2.eff ...]

Options:
  --schema       Comma-separated list of schema files to load
  --verbschema   Comma-separated list of verb schema files to load
  --output       Output file for compiled spec (default: spec.json)
  --verbose      Show detailed output
```

### bundle

Creates a bundle from schemas, verbs, and rules:

```bash
effectusc bundle [options]

Options:
  --name         Bundle name (required)
  --version      Bundle version (default: 1.0.0)
  --desc         Bundle description
  --schema-dir   Directory containing schema files
  --verb-dir     Directory containing verb files
  --rules-dir    Directory containing rule files
  --output       Output file for bundle (default: bundle.json)
  --oci-ref      OCI reference to push bundle to (e.g., ghcr.io/user/bundle:v1)
  --pii-masks    Comma-separated list of PII paths to mask
  --verbose      Show detailed output
```

#### Example: Creating and Pushing a Bundle

```bash
# Create a bundle and save it locally
effectusc bundle \
  --name customer-rules \
  --version 1.2.0 \
  --desc "Customer management rules" \
  --schema-dir ./schemas \
  --verb-dir ./verbs \
  --rules-dir ./rules \
  --output bundle.json

# Create a bundle and push it to an OCI registry
effectusc bundle \
  --name customer-rules \
  --version 1.2.0 \
  --schema-dir ./schemas \
  --verb-dir ./verbs \
  --rules-dir ./rules \
  --oci-ref ghcr.io/myorg/customer-rules:v1.2.0
```

## effectusd

The `effectusd` command is the runtime server that executes bundled rules against facts.

```bash
effectusd [options]

Options:
  # Configuration flags
  --bundle           Path to bundle file
  --oci-ref          OCI reference for bundle (e.g., ghcr.io/user/bundle:v1)
  --plugin-dir       Directory containing verb plugins
  --reload-interval  Interval for hot-reloading (default: 30s)

  # Runtime flags
  --saga             Enable saga-style compensation
  --saga-store       Saga store (memory, redis, postgres) (default: memory)

  # Monitoring flags
  --metrics-addr     Address to expose metrics (default: :9090)
  --pprof-addr       Address to expose pprof (default: :6060)

  # Fact source flags
  --fact-source      Fact source (http, kafka) (default: http)
  --kafka-brokers    Kafka brokers (default: localhost:9092)
  --kafka-topic      Kafka topic (default: facts)

  # HTTP server flags
  --http-addr        HTTP server address (default: :8080)

  # Debug flags
  --verbose          Enable verbose logging
```

### Examples

```bash
# Run with a local bundle file
effectusd --bundle ./bundle.json --verbose

# Run with a bundle from an OCI registry
effectusd --oci-ref ghcr.io/myorg/customer-rules:v1.2.0

# Run with saga compensation enabled
effectusd --bundle ./bundle.json --saga --saga-store postgres

# Run with hot-reloading from OCI registry
effectusd --oci-ref ghcr.io/myorg/customer-rules:latest --reload-interval 60s

# Run with Kafka as the fact source
effectusd --bundle ./bundle.json --fact-source kafka --kafka-topic customer-events
```