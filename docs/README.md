# Effectus Documentation

This directory contains the documentation for Effectus. Start here and follow the paths that match your role.

## Start here

1. `../README.md` - project overview and quick start
2. `GUARANTEES.md` - implemented guarantees and design boundaries
3. `SYSTEM_INTENT.md` - design intent and correctness criteria
4. `GLOSSARY.md` - shared vocabulary
5. `BASICS.md` - facts, verbs, rules, flows
6. `TUTORIALS.md` - short walkthroughs
7. `COMMANDS.md` - CLI reference

## Runtime operations

- `LIFECYCLE.md` - release, activation, generation, refresh, and rollback semantics
- `RUNTIME_CONFIG.md` - YAML config for non-library deployments
- `PRODUCTION_RUNBOOK.md` - production checklist, hotload, rollback
- `FACT_SOURCES.md` - adapters for Kafka/CDC/SQL/S3/Iceberg/AMQP/gRPC
- `GRPC_EXECUTION.md` - gRPC execution interface
- `charts/effectusd/` - Helm chart (OCI)

## Extension system

- `EXTENSION_SYSTEM.md` - JSON and OCI verb schemas and executors
- `coherent_flow.md` - extension to compile to execution flow

## Architecture and theory

- `ARCHITECTURE.md` - system architecture
- `design.md` - detailed design doc
- `theory/` - mathematical foundations

## Examples

- `../examples/coherent_flow/`
- `../examples/proto_driven_development/`
- `../examples/cdc_all/`
- `../examples/flow_ui_demo/`

## Doc guidelines

- Keep docs consistent with the current implementation.
- Add runnable commands and expected outputs where useful.
- Update this index when you add or remove major docs.
