# Extension Loaders

The `loader` package reads schema, function, verb, rule, and OCI extension inputs.

The loader prepares declarations and executor configuration. The compiler still checks rule sources before activation.

## Supported inputs

- Static Go declarations for trusted embedded applications
- JSON verb and schema manifests
- Protobuf verb declarations
- Extension directories
- Digest-pinned OCI bundles
- `.eff` and `.effx` rule sources inside extension snapshots

Read [Extension System](../docs/EXTENSION_SYSTEM.md) for manifest fields and executor targets.

## Extension manager

`ExtensionManager` combines one or more loaders. Registry helpers apply the loaded declarations to candidate schema and verb registries.

Duplicate behavior depends on the configured policy. Production startup uses strict validation and fails on unsupported conflicts.

## Static loaders

Static loaders register Go values directly. They are useful in trusted embedded applications and tests.

Static executors can contain arbitrary Go behavior. They do not become serializable checked IR.

Production effectusd rejects in-process Go plugins.

## JSON manifests

A verb manifest declares contracts and executor targets:

```json
{
  "name": "payments",
  "version": "1.0.0",
  "verbs": [
    {
      "name": "ReservePayment",
      "argTypes": {
        "orderId": "string",
        "amount": "float"
      },
      "requiredArgs": ["orderId", "amount"],
      "returnType": "PaymentReservation",
      "target": {
        "type": "http",
        "config": {
          "url": "https://payments.example/reservations",
          "method": "POST"
        }
      }
    }
  ]
}
```

The extension compiler validates type declarations, required arguments, capabilities, resources, and target configuration.

## Executor targets

Production checked plans support configured HTTP, gRPC, stream, Kafka, and OCI-resolved executors.

HTTP execution applies URL, host, redirect, DNS, response-size, and timeout controls.

Invocation metadata includes stable identity, idempotency key, attempt, contract, and fencing values.

The destination must enforce idempotency or fencing when correctness requires it.

## Extension snapshots

Effectusd builds an immutable extension snapshot for each candidate generation.

The runtime retains a snapshot while an execution uses it. Retirement waits for active references.

A failed candidate releases its resources without changing the active generation.

## OCI loading

Production OCI references must use a digest. Effectusd also requires an operator-provided signature verifier.

The shared archive extractor rejects traversal, links, device entries, excessive file counts, and excessive expanded sizes.

The verifier command defines the trust policy. A successful pull without successful verification is not accepted.

## Rule compilation

The daemon compiles extension `.eff` and `.effx` sources through `compiler.CompileChecked`.

A schema or verb refresh recompiles the source against the candidate environment. A failed compile prevents publication.

## Directory loading

Directory loaders discover supported extension files under configured roots.

Use directory refresh for controlled local development or mounted configuration. Do not use it as a substitute for signed artifact distribution.

## Test

```bash
go test ./loader
```

Use race tests when you change snapshot activation or retirement:

```bash
go test -race ./loader ./runtime
```
