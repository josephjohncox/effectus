# Bundle System

The Effectus Bundle System is a comprehensive packaging and distribution mechanism for rule and flow definitions, along with their associated schemas and verbs. It enables consistent deployment, versioning, and execution of business rules across different environments.

## Overview

A bundle is a self-contained package that includes:

- **Fact schemas**: Type definitions for the data that rules operate on
- **Verb definitions**: Operations that can be performed by rules
- **Rule files**: The business logic expressed as rules
- **Metadata**: Version information, PII masking paths, and runtime requirements

Bundles can be created as local files or distributed via OCI registries (like Docker Hub or GitHub Container Registry).

## Architecture

The bundle system architecture consists of the following components:

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│                 │     │                 │     │                 │
│  Schema System  │────▶│   Verb System   │────▶│   Rule System   │
│                 │     │                 │     │                 │
└─────────────────┘     └─────────────────┘     └─────────────────┘
         │                      │                       │
         │                      │                       │
         ▼                      ▼                       ▼
┌───────────────────────────────────────────────────────────────┐
│                                                               │
│                       Bundle System                           │
│                                                               │
└───────────────────────────────────────────────────────────────┘
                                │
                                │
                 ┌──────────────┴──────────────┐
                 │                             │
                 ▼                             ▼
          ┌─────────────┐             ┌─────────────────┐
          │             │             │                 │
          │ Bundle File │             │  OCI Registry   │
          │             │             │                 │
          └─────────────┘             └─────────────────┘
```

### Key Components

1. **Schema Registry** (`schema.SchemaRegistry`): Manages type definitions from multiple sources (JSON, Protocol Buffers)
2. **Verb Registry** (`schema.VerbRegistry`): Manages verb specifications and their implementations
3. **Bundle Builder** (`unified.BundleBuilder`): Creates bundles from schema, verb, and rule files
4. **OCI Integration** (`unified.OCIBundlePusher/Puller`): Pushes and pulls bundles to/from OCI registries
5. **Runtime** (`cmd/effectusd`): Executes bundled rules against incoming facts

## Bundle Structure

A bundle contains:

```json
{
  "name": "customer-rules",
  "version": "1.2.0",
  "description": "Customer management rules",
  "verbHash": "a1b2c3d4...",
  "createdAt": "2023-06-15T12:34:56Z",
  "schemaFiles": ["customer.json", "address.proto"],
  "verbFiles": ["email.json", "notification.json"],
  "ruleFiles": ["customer_validation.eff", "address_changes.effx"],
  "requiredFacts": ["customer.name", "customer.email"],
  "piiMasks": ["customer.ssn", "payment.cardNumber"]
}
```

- **verbHash**: Hash of all verb specifications, used to validate runtime compatibility
- **requiredFacts**: Facts that must be present for rules to execute
- **piiMasks**: Paths to sensitive information that should be masked in logs

## Using the CLI

The Effectus CLI (`effectusc`) provides commands for working with bundles:

### Creating a Bundle

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

### Pushing to OCI Registry

```bash
effectusc bundle \
  --name customer-rules \
  --version 1.2.0 \
  --schema-dir ./schemas \
  --verb-dir ./verbs \
  --rules-dir ./rules \
  --oci-ref ghcr.io/myorg/customer-rules:v1.2.0
```

### Running with Bundles

The `effectusd` command can run a bundle from either a local file or an OCI registry:

```bash
# From local file
effectusd --bundle ./bundle.json

# From OCI registry
effectusd --oci-ref ghcr.io/myorg/customer-rules:v1.2.0
```

## Development Workflow

The typical workflow for developing and deploying rules:

1. **Define Schemas**: Create JSON or Protocol Buffer schema files defining your fact types
2. **Define Verbs**: Create verb specifications describing available operations
3. **Write Rules**: Create rule files (.eff for list rules, .effx for flow rules)
4. **Build Bundle**: Use `effectusc bundle` to create a bundle
5. **Distribute**: Push the bundle to an OCI registry
6. **Deploy**: Run the bundle with `effectusd`

## Saga Compensation

Bundles support saga-style compensation for transactional operations. If a rule execution fails, the system can automatically compensate for operations that have already succeeded:

```bash
effectusd --bundle ./bundle.json --saga --saga-store postgres
```

Available stores:
- **memory**: In-memory storage (for testing)
- **redis**: Redis-backed storage
- **postgres**: PostgreSQL-backed storage

## Hot Reloading

When running from an OCI registry, `effectusd` can automatically reload the bundle when a new version is available:

```bash
effectusd --oci-ref ghcr.io/myorg/customer-rules:latest --reload-interval 60s
```

## PII Redaction

Bundles can specify paths that contain Personally Identifiable Information (PII). The runtime will automatically mask these values in logs and error messages:

```
Original: {"customer": {"name": "John Smith", "ssn": "123-45-6789"}}
Masked:   {"customer": {"name": "John Smith", "ssn": "***"}}
```

## Conclusion

The Effectus Bundle System provides a robust way to package and distribute business rules. By combining schemas, verbs, and rules into a single artifact, it ensures consistency across environments and simplifies deployment. 