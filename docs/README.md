# Effectus Documentation

Welcome to the Effectus documentation! This directory contains comprehensive documentation for the Effectus typed rule engine.

## Getting Started

Start here if you're new to Effectus:

1. **[Project README](../README.md)** - Overview, quick start, and installation
2. **[Basics](BASICS.md)** - Core concepts: Facts, Verbs, Effects, and rules
3. **[Commands](COMMANDS.md)** - CLI tools reference
4. **[Quick Tutorials](TUTORIALS.md)** - Short walkthroughs for common tasks

## Core Documentation

### Architecture & Design
- **[Architecture](ARCHITECTURE.md)** - Complete system architecture and production deployment ⭐
- **[Design Document](design.md)** - Comprehensive technical design (advanced)
- **[Mathematical Foundations](theory/)** - Category theory, formal semantics, and proofs

### Extension System
- **[Extension System](EXTENSION_SYSTEM.md)** - Unified verb, schema, and bundle system
- **[Coherent Flow](coherent_flow.md)** - Extension loading → compilation → execution

### Integration & Deployment
- **[gRPC Execution Interface](GRPC_EXECUTION.md)** - Standard Facts → Effects interface with rulesets
- **[Client Examples](CLIENT_EXAMPLES.md)** - Multi-language client integration examples
- **[External Fact Sources](FACT_SOURCES.md)** - Streaming + batch tutorials for SQL/Kafka/S3/Iceberg adapters

## Learning Path

### 1. **New Users**
   - Read [Project README](../README.md)
   - Read [Basics](BASICS.md)
   - Try the [coherent flow example](../examples/coherent_flow/)

### 2. **Developers**
   - Read [Architecture](ARCHITECTURE.md) ⭐ **START HERE**
   - Study [Extension System](EXTENSION_SYSTEM.md)
   - Review [coherent flow example](../examples/coherent_flow/)

### 3. **System Integrators**
   - Read [Commands](COMMANDS.md)
   - Study [gRPC Execution Interface](GRPC_EXECUTION.md)
   - Follow [External Fact Sources](FACT_SOURCES.md)
   - Review [Client Examples](CLIENT_EXAMPLES.md)
   - Study deployment patterns in [Architecture](ARCHITECTURE.md)

### 4. **Advanced Users**
   - Read [Design Document](design.md)
   - Study [Theory](theory/) directory
   - Contribute to the codebase

## Key Features

- ✅ **Protocol-First Development** - Schema as single source of truth with buf integration
- ✅ **Multi-Source Data Ingestion** - Kafka, HTTP, SQL/Snowflake, S3, Iceberg, Database, Redis, File adapters
- ✅ **Static Validation** - All rules validated before runtime with comprehensive type checking
- ✅ **VS Code Integration** - Full language support with IntelliSense and hot reload
- ✅ **Modern SQL Storage** - Type-safe queries with sqlc and automatic migrations with goose
- ✅ **Production-Ready** - OCI bundles, distributed locking, saga compensation, observability

## Examples

Working examples demonstrating Effectus features:

- **[Protocol-Driven Development](../examples/proto_driven_development/)** - Schema-first development workflow
- **[Multi-Source Ingestion](../examples/multi_source_ingestion/)** - Universal data ingestion examples
- **[Extension System](../examples/extension_system/)** - Static and dynamic extension loading
- **[Business Examples](../examples/)** - Domain-specific rule examples

## Contributing to Documentation

We welcome documentation improvements! Please:

1. Keep documentation accurate to the actual implementation
2. Follow the established structure and writing style
3. Include practical examples where helpful
4. Update this README if your changes affect the documentation structure

## Need Help?

- Check the [Project README](../README.md) for community links
- Review [Commands](COMMANDS.md) for CLI help
- Study working [examples](../examples/)
- Read the [Architecture](ARCHITECTURE.md) for comprehensive system overview

This documentation reflects the current implementation of Effectus and its production-ready architecture. 
