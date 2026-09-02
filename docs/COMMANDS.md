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
effectusc parse --verbose rules/customer.eff rules/payment.eff
```

#### typecheck

Parses and type checks rule files against schemas.

```bash
effectusc typecheck [options] file1.eff [file2.eff ...]

Options:
  --schema       Comma-separated list of schema files to load
  --schema-sources Path to schema sources config (YAML/JSON)
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
  --schema-sources Path to schema sources config (YAML/JSON)
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
  --check   Return non-zero if files need formatting; never write files
```

Plain `effectusc format` writes by default. `--check` is unconditionally read-only, even though `--write` defaults to true.

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
  --schema         Comma-separated list of schema files or directories
  --schema-sources Path to schema sources config (YAML/JSON)
  --format         Output format: json or dot (default: json)
  --output         Output file for the graph (defaults to stdout)
  --verbose        Show detailed output
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
  --schema         Comma-separated list of schema files or directories
  --schema-sources Path to schema sources config (YAML/JSON)
  --format         Output format: text or json (default: text)
  --output         Output file for the report (defaults to stdout)
  --verbose        Show detailed output
```

**Example:**

```bash
effectusc facts --schema schemas/ rules/*.eff
```

#### compile

Compiles rule files into a checked IR protobuf artifact. The artifact is validated against the supplied schema and verb declarations.

```bash
effectusc compile [options] file1.eff [file2.eff ...]

Options:
  --schema       Comma-separated list of schema files to load
  --schema-sources Path to schema sources config (YAML/JSON)
  --verbschema   Comma-separated list of verb schema files to load
  --output       Output file for checked IR (default: rules.effir)
  --verbose      Show detailed output
```

**Example:**

```bash
effectusc compile \
  --schema schemas/ \
  --verbschema verbs/ \
  --output customer-rules.effir \
  rules/*.eff
```

#### migrate-workflows

Converts one legacy JSON workflow manifest to `.effx` source. By default, the command prints the source.

```bash
effectusc migrate-workflows [--output workflow.effx] legacy-workflows.json
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
  --schema-sources Path to schema sources config (YAML/JSON)
  --verb-dir     Directory containing verb files
  --rules-dir    Directory containing rule files
  --output       Output file for bundle (default: bundle.json)
  --oci-ref      OCI publication target; prints the digest-pinned reference to deploy
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

The command always writes the local source bundle. `--oci-ref` may name a mutable publication tag, but the command verifies the upload and prints the only reference that may be deployed: `repository@sha256:...`.

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

The `effectusd` command compiles embedded `.eff` and `.effx` sources to checked IR and routes HTTP, Kafka, and generated gRPC requests through one durable execution engine. It requires PostgreSQL through `EFFECTUS_POSTGRES_DSN`.

### Usage

```bash
effectusd [options]
```

### Options

#### Bundle Configuration

```bash
--bundle           Path to bundle file
--oci-ref          Digest-pinned source-bundle OCI reference
--oci-cache-dir    Writable OCI cache directory
--oci-signature-verifier Fixed verifier executable for OCI signatures
--extensions-dir   Directory containing extension manifests (*.verbs.json, *.schema.json)
--verb-dir         Deprecated alias for --extensions-dir (emits a startup notice)
--extensions-oci   OCI references for extension bundles (comma-separated)
--extensions-reload-interval Rejected compatibility flag. Redeploy to change extensions.
--schema-sources   Path to schema sources config (YAML/JSON)
--config           Path to YAML/JSON config file
--reload-interval  Rejected compatibility flag. Redeploy to change the bundle.
--verb-duplicate-policy Duplicate verb policy (error, replace, ignore)
--verb-oci-warmup  Deprecated compatibility flag until 2027-09-01; OCI executor resolvers are not supported
--verb-strict      Validate verb arguments and return values (default: true)
```

#### Runtime Configuration

```bash
--fixed-time       Fixed time for deterministic evaluation (RFC3339/RFC3339Nano)
```

PostgreSQL is required for every daemon transport. Explicit legacy saga-store, Redis, and plugin settings are rejected with migration guidance; they are not runtime choices.

#### Fact Sources

```bash
--fact-source             Fact source (http, kafka) (default: http)
--kafka-brokers           Kafka brokers (default: localhost:9092)
--kafka-topic             Kafka topic (default: facts)
--kafka-consumer-group    Kafka consumer group (default: effectusd)
--kafka-cluster-namespace Stable cluster name used in delivery IDs
--kafka-ack-contract      Acknowledgement contract (default: completed_processing)
--kafka-max-attempts      Attempts before poison handling (default: 3)
--kafka-retry-initial     Initial same-record retry delay (default: 1s)
--kafka-retry-max         Maximum same-record retry delay (default: 30s)
--kafka-poison-policy     Poison policy: halt, skip, or dlq (default: halt)
--kafka-dlq-topic         DLQ topic for the dlq policy
--kafka-dlq-mode          Explicit DLQ delivery contract
```

#### Server Configuration

```bash
--http-addr        HTTP server address (default: :8080)
--grpc-addr        Generated gRPC execution address (empty disables it)
--grpc-tls-cert    PEM certificate for gRPC
--grpc-tls-key     PEM private key for gRPC
--grpc-allow-insecure Explicitly permit plaintext gRPC
--grpc-max-receive-bytes Maximum request size
--grpc-max-send-bytes Maximum response size
--grpc-max-execution-duration Maximum execution duration
--grpc-max-concurrent Maximum concurrent executions
--metrics-addr     Address to expose metrics (default: :9090)
--shutdown-timeout Deadline for graceful shutdown and worker drain (default: 30s)
```

Supply the PostgreSQL ledger DSN through `EFFECTUS_POSTGRES_DSN`. The daemon rejects a DSN supplied on the command line because process arguments can expose secrets.

```bash
--database-migrations      Migration mode: validate, validate-only, apply, or legacy-apply
--database-max-open        Maximum open PostgreSQL connections
--database-max-idle        Maximum idle PostgreSQL connections
--database-max-lifetime    Maximum PostgreSQL connection lifetime
--database-max-idle-time   Maximum PostgreSQL connection idle time
--admin-prune-before       RFC3339 cutoff for terminal record pruning
--admin-prune-batch-size   Maximum rows in a prune batch
--admin-prune-dry-run      Report candidates without deletion
--admin-prune-backup-verified Confirm a restore-verified backup before deletion
```

#### API Security + Rate Limits

```bash
--api-auth             API auth mode (token, disabled)
--api-token            Rejected; use EFFECTUS_API_TOKEN
--api-read-token       Rejected; use EFFECTUS_API_READ_TOKEN
--api-acl-file         Path to API ACL file (YAML/JSON)
--api-rate-limit       Requests per minute per client (0 to disable)
--api-rate-burst       Burst size (0 to use rate limit)
--api-limiter-capacity Maximum active client limiter buckets
--api-limiter-idle-ttl Idle time before a limiter bucket expires
--trusted-proxy-cidrs  Proxy CIDRs trusted to supply X-Forwarded-For
--rules-hotload        Rejected compatibility flag. Use /api/rules/validate without it.
--rules-history        Number of hotload bundles to keep (0 to disable)
--rules-history-dir    Directory for bundle history snapshots
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
EFFECTUS_POSTGRES_DSN="postgres://effectus:...@db/effectus?sslmode=require" \
  effectusd --bundle ./bundle.json --verbose
```

#### Run with OCI Registry Bundle

```bash
EFFECTUS_POSTGRES_DSN="postgres://effectus:...@db/effectus?sslmode=require" \
  effectusd \
  --bundle /config/customer-rules.json
```

#### Durable checked workflows

Effectusd uses checked workflows and the V2 outbox by default. The old `--saga` flag is rejected because it selects the obsolete callback implementation. Embedded callers use `runtime.Engine` or `ExecuteWorkflowWithIdentity` with a configured durable store.

#### Deploy a new OCI generation

OCI references are immutable and digest-pinned. Publish, sign, and deploy a new digest instead of polling a mutable tag.

#### Status UI and Playground

```bash
EFFECTUS_API_TOKEN=devtoken \
EFFECTUS_POSTGRES_DSN="postgres://effectus:...@db/effectus?sslmode=require" \
  effectusd --bundle ./bundle.json --http-addr :8080
# open http://localhost:8080/ui
```

#### Download current bundle

```bash
curl -H 'Authorization: Bearer devtoken' http://localhost:8080/api/bundle > bundle.json
```

#### Metrics endpoint (Prometheus)

```bash
curl http://localhost:9090/metrics
```

#### Candidate validation payload

The validation endpoint accepts an optional `canary` block to run a dry-run diff against the active bundle:

```json
{
  "path": "rules/fraud_rules.eff",
  "format": "eff",
  "content": "...",
  "replace": true,
  "canary": {
    "universe": "default",
    "mode": "both",
    "use_stored": true,
    "facts": {
      "transaction": {"amount": 1200, "id": "abc"},
      "customer": {"risk_score": 90}
    }
  }
}
```

Candidate validation is available without a mutation flag. Checked daemon activation and rollback remain fail-closed. Deploy a new immutable generation to apply changes.

Post facts for a universe projection. Reuse the same `Idempotency-Key` and identical payload when retrying one logical request. Use a new key for a new submission.

```bash
curl --fail-with-body -X POST http://localhost:8080/api/facts \
  -H 'Authorization: Bearer devtoken' \
  -H 'Idempotency-Key: customer-order-42-v1' \
  -H 'Content-Type: application/json' \
  -d '{"universe":"prod-projection","namespace":"tenant-a","facts":{"customer":{"tier":"gold"},"order":{"total":120}}}'
```

`namespace` is the durable tenant identity used in admission and execution IDs. `universe` is the local projection key. For compatibility, omitting `namespace` uses `universe` as the namespace (or `default` when both are empty).

#### Use Kafka as Fact Source

```bash
EFFECTUS_POSTGRES_DSN="postgres://effectus:...@db/effectus?sslmode=require" \
effectusd \
  --bundle ./bundle.json \
  --fact-source kafka \
  --kafka-ack-contract durable_acceptance \
  --kafka-brokers kafka1:9092,kafka2:9092 \
  --kafka-topic customer-events
```

#### Full production configuration

```bash
EFFECTUS_API_TOKEN="..." \
EFFECTUS_POSTGRES_DSN="postgres://effectus:...@db/effectus?sslmode=require" \
effectusd \
  --bundle /config/customer-rules.json \
  --fact-source kafka \
  --kafka-ack-contract durable_acceptance \
  --kafka-brokers kafka-cluster:9092 \
  --kafka-topic events \
  --http-addr :8080 \
  --metrics-addr :9090 \
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
# Create a canonical source bundle
effectusc bundle \
  --name my-rules \
  --version 1.0.0 \
  --schema-dir schemas/ \
  --verb-dir verbs/ \
  --rules-dir rules/ \
  --output my-rules.json
```

### 4. Runtime Deployment

```bash
# Run the checked durable daemon
EFFECTUS_POSTGRES_DSN="postgres://effectus:...@db/effectus?sslmode=require" \
effectusd \
  --bundle /config/my-rules.json \
  --fact-source kafka \
  --kafka-ack-contract durable_acceptance
```

### HTTP Endpoints

```bash
# Liveness
GET /healthz

# Readiness (bundle loaded)
GET /readyz

# API status (requires token when auth is enabled)
GET /api/status
```

Notes:

- `/healthz` and `/readyz` are unauthenticated by default.
- `/api/*` endpoints are protected and rate-limited when auth is enabled.

## Error Handling

All commands return appropriate exit codes:

- **0**: Success
- **1**: Error (compilation failure, invalid arguments, etc.)

Error messages are written to stderr, while normal output goes to stdout.

## Configuration Files

Effectusd accepts strict YAML or JSON through `--config`. Unknown fields and multiple documents are rejected. Explicit CLI flags override non-secret config values.

## Secret Environment Variables

Effectusd reads these secrets from the environment:

- `EFFECTUS_API_TOKEN`
- `EFFECTUS_API_READ_TOKEN`
- `EFFECTUS_POSTGRES_DSN`

The corresponding secret command-line flags are rejected because process arguments can expose their values.

## Integration Examples

### CI/CD Pipeline

```bash
#!/bin/bash
set -e

# Validate rules
effectusc typecheck --schema schemas/ --verbschema verbs/ rules/*.eff

# Create the source bundle
effectusc bundle \
  --name "app-rules" \
  --version "$BUILD_VERSION" \
  --schema-dir schemas/ \
  --verb-dir verbs/ \
  --rules-dir rules/ \
  --output "dist/app-rules-$BUILD_VERSION.json"

echo "Bundle created successfully"
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
  --set image.digest=sha256:IMAGE_DIGEST \
  --set bundle.ociRef=ghcr.io/myorg/bundles/order-review@sha256:BUNDLE_DIGEST \
  --set bundle.signatureVerifier=/usr/local/bin/effectus-verify-oci \
  --set postgres.existingSecret=effectusd-postgres \
  --set api.existingSecret=effectusd-api
```

This documentation reflects the current implementation and capabilities of the Effectus CLI tools.
