# Effectus

![Effectus logo](./effectus-small.png)

Effectus compiles `.eff` and `.effx` rules to checked protobuf IR. The same execution engine supports process-local embedded use and PostgreSQL-backed durable execution.

## Start here

Choose one first-run path in the [Getting Started guide](docs/GETTING_STARTED.md):

| Path | Choose it when |
| --- | --- |
| Embedded Go | A Go service owns rules, handlers, and process lifecycle |
| Durable Docker | Execution state and business effects must survive service restarts |

Both paths use the same order-review rule and scenario artifact. Each path proves matching replay and one business review.

## Production boundary

Effectus controls durable admission and internal execution state. It does not make an external service transactional.

External destinations must coordinate deduplication with their business commit and enforce fencing when required. Fencing does not replace deduplication. Compensation is recovery work, not an ACID rollback.

Read [Runtime Guarantees](docs/GUARANTEES.md) before a production deployment.

## Install

Use binaries, checksums, SBOMs, signatures, and images from the [latest release](https://github.com/josephjohncox/effectus/releases/latest).

Do not use a mutable `@main` Go dependency for production. Pin a release tag or commit.

## Documentation

- [Getting started](docs/GETTING_STARTED.md)
- [Integration guide](docs/INTEGRATION.md)
- [Supported Go API and ownership contracts](docs/go-api.md)
- [gRPC capability matrix](docs/grpc-capabilities.md)
- [v0.3 Go compatibility](docs/COMPATIBILITY.md)
- [Effectus basics](docs/BASICS.md)
- [CLI reference](docs/COMMANDS.md)
- [HTTP API reference](docs/HTTP_API.md)
- [Runtime configuration](docs/RUNTIME_CONFIG.md)
- [Runtime guarantees](docs/GUARANTEES.md)
- [Production runbook](docs/PRODUCTION_RUNBOOK.md)
- [Examples](examples/README.md)
- [Published documentation](https://josephjohncox.github.io/effectus/)

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for repository workflows and test requirements.

## License

Effectus uses the MIT license. See [LICENSE](LICENSE).
