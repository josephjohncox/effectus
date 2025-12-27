# Effectus CLI Commands

This document describes the command-line tools available in Effectus.

## Overview

Effectus provides two main CLI tools:
- **`effectusc`**: Compiler and development utilities
- **`effectusd`**: Runtime daemon for executing rules

## effectusc - Compiler & Development Tools

The `effectusc` command provides development and compilation utilities for Effectus rules.

### Usage

```bash
effectusc <command> [options] [files...]
```

### Available Commands

#### parse

Parses rule files without type checking them.

```bash
effectusc parse [options] file1.eff [file2.eff ...]

Options:
  --verbose      Show detailed output
```

**Example:**
```bash
effectusc parse rules/customer.eff rules/payment.eff --verbose
```

#### typecheck

Parses and type checks rule files against schemas.

```bash
effectusc typecheck [options] file1.eff [file2.eff ...]

Options:
  --schema       Comma-separated list of schema files to load
  --verbschema   Comma-separated list of verb schema files to load
  --output       Output file for reports (defaults to stdout)
  --report       Generate type report
  --verbose      Show detailed output
```

**Example:**
```bash
effectusc typecheck \
  --schema schemas/customer.json,schemas/payment.json \
  --verbschema verbs/email.json \
  --report \
  rules/customer.eff
```

#### check

Runs parse + type check + lint checks in one command.

```bash
effectusc check [options] file1.eff [file2.eff ...]

Options:
  --schema       Comma-separated list of schema files to load
  --verbschema   Comma-separated list of verb schema files to load
  --format       Output format: text or json (default: text)
  --fail-on-warn Return non-zero exit code when warnings are present
  --unsafe       Unsafe expression policy: warn, error, ignore (default: warn)
  --verbs        Verb lint policy: error, warn, ignore (default: error)
  --verbose      Show detailed output
```

**Example:**
```bash
effectusc check \
  --schema schemas/customer.json,schemas/payment.json \
  --verbschema verbs/email.json \
  --format text \
  rules/customer.eff
```

#### lsp

Starts the Effectus Language Server (stdio). This is used by the VS Code extension.

```bash
effectusc lsp
```

#### format

Formats `.eff` and `.effx` files into a canonical layout.

```bash
effectusc format [options] file1.eff [file2.effx ...]

Options:
  --write   Write formatted output back to files (default: true)
  --stdout  Print formatted output to stdout
  --check   Return non-zero exit code if files need formatting
```

**Example:**
```bash
effectusc format --check rules/*.eff
```

**Example (bound flow formatting):**
```bash
effectusc format --stdout rules/case_hold.effx
```

Input:
```effx
flow "CaseHold" priority 5 { when { order.amount>1000 } steps { caseId=OpenCase(orderId:order.id,reason:"risk") UpdateCase(caseId:$caseId,status:"held") } }
```

Output:
```effx
flow "CaseHold" priority 5 {
  when {
    order.amount > 1000
  }
  steps {
    caseId = OpenCase(orderId: order.id, reason: "risk")
    UpdateCase(caseId: $caseId, status: "held")
  }
}
```

#### graph

Emits a dependency graph (rules/flows → facts/verbs) plus fact coverage.

```bash
effectusc graph [options] file1.eff [file2.effx ...]

Options:
  --schema  Comma-separated list of schema files or directories
  --format  Output format: json or dot (default: json)
  --output  Output file for the graph (defaults to stdout)
  --verbose Show detailed output
```

**Example:**
```bash
effectusc graph --schema schemas/ --format dot rules/*.eff
```

#### facts

Emits a fact coverage report (used/unknown/unused) across rules and flows.

```bash
effectusc facts [options] file1.eff [file2.effx ...]

Options:
  --schema  Comma-separated list of schema files or directories
  --format  Output format: text or json (default: text)
  --output  Output file for the report (defaults to stdout)
  --verbose Show detailed output
```

**Example:**
```bash
effectusc facts --schema schemas/ rules/*.eff
```

#### compile

Compiles rule files into a unified specification.

```bash
effectusc compile [options] file1.eff [file2.eff ...]

Options:
  --schema       Comma-separated list of schema files to load
  --verbschema   Comma-separated list of verb schema files to load
  --output       Output file for compiled spec (default: spec.json)
  --verbose      Show detailed output
```

**Example:**
```bash
effectusc compile \
  --schema schemas/ \
  --verbschema verbs/ \
  --output customer-rules.json \
  rules/*.eff
```

#### bundle

Creates a bundle from schemas, verbs, and rules for distribution.

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

**Examples:**

Create local bundle:
```bash
effectusc bundle \
  --name customer-rules \
  --version 1.2.0 \
  --desc "Customer management rules" \
  --schema-dir ./schemas \
  --verb-dir ./verbs \
  --rules-dir ./rules \
  --pii-masks customer.ssn,payment.cardNumber \
  --output bundle.json
```

Create and push to OCI registry:
```bash
effectusc bundle \
  --name customer-rules \
  --version 1.2.0 \
  --schema-dir ./schemas \
  --verb-dir ./verbs \
  --rules-dir ./rules \
  --oci-ref ghcr.io/myorg/customer-rules:v1.2.0
```

#### resolve

Resolves bundle dependencies from an extension manifest (including registry lookups and checksum verification).

```bash
effectusc resolve [options] manifest.json

Options:
  --manifest         Path to extension manifest (defaults to first arg)
  --cache            Bundle cache directory (defaults to EFFECTUS_BUNDLE_CACHE or ./bundles)
  --registry         Registry override(s): name=base or base (comma-separated)
  --default-registry Default registry name
  --engine-version   Effectus engine version for compatibility checks
  --verify           Verify bundle checksums when provided (default: true)
  --format           Output format: text or json (default: text)
```

**Example:**
```bash
effectusc resolve \
  --registry public=ghcr.io/myorg \
  --engine-version 1.4.0 \
  ./extensions.json
```

#### capabilities

Analyzes verb capabilities in rule files.

```bash
effectusc capabilities [options] file1.eff [file2.eff ...]

Options:
  --output       Output file for analysis report (defaults to stdout)
  --verbose      Show detailed output
```

**Example:**
```bash
effectusc capabilities \
  --output capability-report.md \
  rules/*.eff
```

## effectusd - Runtime Daemon

The `effectusd` command is the runtime server that executes bundled rules against facts.

### Usage

```bash
effectusd [options]
```

### Options

#### Bundle Configuration
```bash
--bundle           Path to bundle file
--oci-ref          OCI reference for bundle (e.g., ghcr.io/user/bundle:v1)
--plugin-dir       Directory containing verb plugins
--reload-interval  Interval for hot-reloading (default: 30s)
```

#### Runtime Configuration
```bash
--saga             Enable saga-style compensation
--saga-store       Saga store (memory, redis, postgres) (default: memory)
```

#### Fact Sources
```bash
--fact-source      Fact source (http, kafka) (default: http)
--kafka-brokers    Kafka brokers (default: localhost:9092)
--kafka-topic      Kafka topic (default: facts)
```

#### Server Configuration
```bash
--http-addr        HTTP server address (default: :8080)
--metrics-addr     Address to expose metrics (default: :9090)
--pprof-addr       Address to expose pprof (default: :6060)
```

#### API Security + Rate Limits
```bash
--api-auth             API auth mode (token, disabled)
--api-token            Write token for /api endpoints (comma-separated)
--api-read-token       Read-only token for /api endpoints (comma-separated)
--api-acl-file         Path to API ACL file (YAML/JSON)
--api-rate-limit       Requests per minute per client (0 to disable)
--api-rate-burst       Burst size (0 to use rate limit)
```

Example ACL file: `docs/acl.example.yml`.

#### Facts Store
```bash
--facts-store           Facts store (file, memory)
--facts-path            Facts store path (file store)
--facts-merge-default   Default merge strategy (first, last, error)
--facts-merge-namespace Namespace-specific merge strategy (namespace=first|last|error)
--facts-cache-policy    Facts cache policy (none, lru)
--facts-cache-max-universes   Max universes to keep (0 for unlimited)
--facts-cache-max-namespaces  Max namespaces per universe (0 for unlimited)
```

#### Debug Options
```bash
--verbose          Enable verbose logging
```

### Examples

#### Run with Local Bundle

```bash
effectusd --bundle ./bundle.json --verbose
```

#### Run with OCI Registry Bundle

```bash
effectusd --oci-ref ghcr.io/myorg/customer-rules:v1.2.0
```

#### Enable Saga Compensation

```bash
effectusd \
  --bundle ./bundle.json \
  --saga \
  --saga-store postgres
```

#### Hot Reload from OCI Registry

```bash
effectusd \
  --oci-ref ghcr.io/myorg/customer-rules:latest \
  --reload-interval 60s \
  --verbose
```

#### Status UI and Playground

```bash
effectusd --bundle ./bundle.json --http-addr :8080 --api-token devtoken
# open http://localhost:8080/ui
```

Post facts for a universe snapshot:

```bash
curl -X POST http://localhost:8080/api/facts \
  -H 'Authorization: Bearer devtoken' \
  -H 'Content-Type: application/json' \
  -d '{"universe":"prod","facts":{"customer":{"tier":"gold"},"order":{"total":120}}}'
```

#### Use Kafka as Fact Source

```bash
effectusd \
  --bundle ./bundle.json \
  --fact-source kafka \
  --kafka-brokers kafka1:9092,kafka2:9092 \
  --kafka-topic customer-events
```

#### Full Production Configuration

```bash
effectusd \
  --oci-ref ghcr.io/myorg/customer-rules:v1.0.0 \
  --saga \
  --saga-store postgres \
  --fact-source kafka \
  --kafka-brokers kafka-cluster:9092 \
  --kafka-topic events \
  --http-addr :8080 \
  --metrics-addr :9090 \
  --reload-interval 300s \
  --verbose
```

## Development Workflow

### 1. Development Phase

```bash
# Parse rules during development
effectusc parse rules/*.eff

# Type check with schemas
effectusc typecheck \
  --schema schemas/ \
  --verbschema verbs/ \
  rules/*.eff
```

### 2. Compilation Phase

```bash
# Compile rules into a spec
effectusc compile \
  --schema schemas/ \
  --verbschema verbs/ \
  --output compiled-rules.json \
  rules/*.eff

# Analyze capabilities
effectusc capabilities rules/*.eff
```

### 3. Bundle Creation

```bash
# Create distributable bundle
effectusc bundle \
  --name my-rules \
  --version 1.0.0 \
  --schema-dir schemas/ \
  --verb-dir verbs/ \
  --rules-dir rules/ \
  --oci-ref ghcr.io/myorg/my-rules:v1.0.0
```

### 4. Runtime Deployment

```bash
# Run in production
effectusd \
  --oci-ref ghcr.io/myorg/my-rules:v1.0.0 \
  --saga \
  --saga-store postgres \
  --fact-source kafka
```

## Error Handling

All commands return appropriate exit codes:
- **0**: Success
- **1**: Error (compilation failure, invalid arguments, etc.)

Error messages are written to stderr, while normal output goes to stdout.

## Configuration Files

Currently, all configuration is done via command-line flags. Future versions may support configuration files for complex deployments.

## Environment Variables

The following environment variables are respected:

- `EFFECTUS_VERBOSE`: Set to "true" to enable verbose output globally
- `EFFECTUS_BUNDLE_CACHE`: Directory for caching OCI bundles
- `EFFECTUS_BUNDLE_REGISTRY`: Default bundle registry base (e.g., ghcr.io/myorg)
- `EFFECTUS_BUNDLE_REGISTRIES`: Additional registries as name=base pairs (comma-separated)
- `EFFECTUS_PLUGIN_PATH`: Additional directories to search for verb plugins
- `EFFECTUS_UNSAFE_MODE`: Unsafe expression policy for linting (warn, error, ignore)

## Integration Examples

### CI/CD Pipeline

```bash
#!/bin/bash
set -e

# Validate rules
effectusc typecheck --schema schemas/ --verbschema verbs/ rules/*.eff

# Create bundle
effectusc bundle \
  --name "app-rules" \
  --version "$BUILD_VERSION" \
  --schema-dir schemas/ \
  --verb-dir verbs/ \
  --rules-dir rules/ \
  --oci-ref "ghcr.io/myorg/app-rules:$BUILD_VERSION"

echo "Bundle created and pushed successfully"
```

### Docker Deployment

```dockerfile
FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY effectusd /usr/local/bin/
EXPOSE 8080 9090
CMD ["effectusd", "--oci-ref", "ghcr.io/myorg/rules:latest"]
```

### OCI + Helm Publishing

```bash
# Build and push runtime image
docker build -t ghcr.io/myorg/effectusd:v1.2.3 .
docker push ghcr.io/myorg/effectusd:v1.2.3

# Package and push Helm chart (OCI)
helm package charts/effectusd --version 1.2.3 --app-version 1.2.3 -d dist
helm push dist/effectusd-1.2.3.tgz oci://ghcr.io/myorg/helm

# Install from GHCR (OCI)
helm install effectusd oci://ghcr.io/myorg/helm/effectusd \
  --version 1.2.3 \
  --set bundle.ociRef=ghcr.io/myorg/bundles/flow-ui-demo:1.2.3
```

This documentation reflects the current implementation and capabilities of the Effectus CLI tools.
