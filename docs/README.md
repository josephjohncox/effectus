# Effectus Documentation

Welcome to the Effectus documentation! This directory contains comprehensive documentation for the Effectus typed rule engine.

## Getting Started

Start here if you're new to Effectus:

1. **[Project README](../README.md)** - Overview, quick start, and installation
2. **[Basics](BASICS.md)** - Core concepts: Facts, Verbs, Effects, and architecture
3. **[Commands](COMMANDS.md)** - CLI tools reference

## Architecture & Design

Understand how Effectus works:

- **[Architecture](ARCHITECTURE.md)** - High-level system design and flow
- **[Coherent Flow](coherent_flow.md)** - Extension loading → compilation → execution
- **[Design Document](design.md)** - Comprehensive technical design (advanced)

## System Components

Deep dive into specific subsystems:

- **[Extension System](EXTENSION_SYSTEM.md)** - Unified verb, schema, and bundle system
- **[gRPC Execution Interface](GRPC_EXECUTION.md)** - Standard Facts → Effects interface with rulesets
- **[Client Examples](CLIENT_EXAMPLES.md)** - Multi-language client integration examples
- **[Mathematical Foundations](theory/)** - Category theory, formal semantics, and proofs

## Documentation Structure

### Quick Reference
- **[Basics](BASICS.md)** - Start here for core concepts
- **[Commands](COMMANDS.md)** - CLI reference for daily use

### Architecture
- **[Architecture](ARCHITECTURE.md)** - System overview and flow
- **[Coherent Flow](coherent_flow.md)** - Modern extension and execution system
- **[Design](design.md)** - Complete technical specification

### Detailed Guides  
- **[Extension System](EXTENSION_SYSTEM.md)** - Unified system for verbs, schemas, and bundles
- **[gRPC Execution Interface](GRPC_EXECUTION.md)** - Standard interface for rule execution
- **[Client Examples](CLIENT_EXAMPLES.md)** - Multi-language client implementations

### Advanced Topics
- **[Mathematical Foundations](theory/)** - Category theory, formal semantics, and proofs

## Learning Path

### 1. **New Users**
   - Read [Project README](../README.md)
   - Read [Basics](BASICS.md)
   - Try the [coherent flow example](../examples/coherent_flow/)

### 2. **Developers**
   - Read [Architecture](ARCHITECTURE.md)
   - Read [Coherent Flow](coherent_flow.md)
   - Study [Extension System](EXTENSION_SYSTEM.md)

### 3. **System Integrators**
   - Read [Commands](COMMANDS.md)
   - Read [Extension System](EXTENSION_SYSTEM.md)
   - Study [Client Examples](CLIENT_EXAMPLES.md)
   - Review deployment patterns in [Architecture](ARCHITECTURE.md)

### 4. **Advanced Users**
   - Read [Design Document](design.md)
   - Study [Theory](theory/) directory
   - Contribute to the codebase

## Key Features Documented

- ✅ **Static Validation** - All rules validated before runtime
- ✅ **Coherent Flow** - Extension loading → compilation → execution  
- ✅ **Multiple Executors** - Local, HTTP, gRPC, message queue execution
- ✅ **Type Safety** - Protocol Buffer schemas with path validation
- ✅ **Capability System** - Fine-grained security and resource control
- ✅ **Hot Reload** - Update rules without restarting services
- ✅ **Bundle Distribution** - OCI registry packaging and distribution

## Contributing to Documentation

We welcome documentation improvements! Please:

1. Keep documentation accurate to the actual implementation
2. Follow the established structure and writing style
3. Include practical examples where helpful
4. Update multiple documents if your changes affect concepts covered elsewhere

## Examples

Working examples demonstrating Effectus features:

- **[Client Examples](CLIENT_EXAMPLES.md)** - Multi-language gRPC client implementations
- **[Coherent Flow Example](../examples/coherent_flow/)** - Complete working demonstration
- **[Extension System Example](../examples/extension_system/)** - Static and dynamic extensions
- **[Business Examples](../examples/)** - Domain-specific rule examples

## Need Help?

- Check the [Project README](../README.md) for community links
- Review [Commands](COMMANDS.md) for CLI help
- Study working [examples](../examples/)
- Read the [Design Document](design.md) for detailed technical information

This documentation reflects the current implementation of Effectus and its coherent flow architecture. 