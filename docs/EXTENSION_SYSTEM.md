# Extension System

Effectus extensions add fact types, pure functions, verb contracts, executor targets, and rule sources.

Effectusd compiles extension `.eff` and `.effx` sources into canonical checked IR before it publishes a generation.

## Production boundary

Production effectusd accepts declarative extension inputs. It rejects in-process Go plugins.

A trusted embedded Go application can register static Go executors. This compatibility path is outside the daemon isolation boundary.

JSON verb manifests define contracts and targets. They do not contain workflow control flow.

## Extension inputs

Effectus supports:

- JSON verb manifests
- JSON schema and function manifests
- Protobuf verb declarations
- Local extension directories
- Digest-pinned OCI extension bundles
- Static Go declarations for embedded applications

The extension manager combines these inputs into a candidate declaration environment.

## Verb manifest

A verb manifest declares one or more operation contracts:

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
      "capabilities": ["write", "idempotent"],
      "resources": [
        {
          "resource": "payment",
          "capabilities": ["write"]
        }
      ],
      "inverse": "ReleasePayment",
      "target": {
        "type": "http",
        "config": {
          "url": "https://payments.example/reservations",
          "method": "POST",
          "timeout": "5s"
        }
      }
    }
  ]
}
```

The compiler checks names, argument types, required arguments, result types, capabilities, resources, inverse references, and target configuration.

A successful declaration check does not prove the destination implements the contract.

## Schema manifest

A schema manifest can define named types, pure function signatures, and immutable initial data:

```json
{
  "name": "payment-types",
  "version": "1.0.0",
  "types": {
    "PaymentReservation": {
      "name": "PaymentReservation",
      "type": "object",
      "properties": {
        "reservationId": {"type": "string"},
        "accepted": {"type": "boolean"}
      }
    }
  },
  "functions": {
    "isSupportedCurrency": {
      "name": "isSupportedCurrency",
      "type": "builtin"
    }
  },
  "initialData": {
    "payment.defaultCurrency": "USD"
  }
}
```

The current workflow IR records function declarations as generation metadata. It does not invoke arbitrary registered Go functions.

## Rule sources

An extension bundle can contain `.eff` and `.effx` sources.

The daemon compiles these sources with `compiler.CompileChecked`. It rejects a bundle when any source fails to parse or check.

A schema or verb refresh recompiles the same source against the candidate environment.

## Executor targets

### HTTP

The HTTP executor sends a checked argument object to a configured endpoint.

It applies URL, host, redirect, DNS, timeout, and response-size controls. These controls reduce SSRF and resource-exhaustion risk.

The destination receives invocation, idempotency, attempt, contract, and fencing metadata.

### gRPC

The gRPC executor invokes a configured unary method with typed `Struct` payloads.

It applies transport security policy, deadlines, message limits, and strict result validation.

### Stream and Kafka

Stream targets publish checked payloads to a configured publisher. Kafka publication waits for the configured broker acknowledgement.

Publication acknowledgement does not prove that a downstream consumer applied the operation.

### OCI-resolved executor

An OCI target resolves executor configuration from a verified extension bundle.

The reference must use a digest. The operator-provided verifier must approve that digest before activation.

## Static executors

A trusted embedded application can register a `loader.VerbExecutor`:

```go
type VerbExecutor interface {
    Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}
```

Static executors can contain arbitrary Go code and process state. They cannot become checked protobuf IR.

Do not expose this path as an untrusted plugin system.

## Immutable snapshots

The runtime builds one extension snapshot for each candidate generation.

A snapshot contains declarations, executor instances, checked artifacts, and content digests. Active executions retain a reference to their snapshot.

Retirement waits until no execution uses the old snapshot. A failed candidate closes its own resources without changing the active generation.

## OCI distribution

Create and push an extension bundle with `effectusc`:

```bash
effectusc bundle \
  --name payments \
  --version 1.0.0 \
  --schema-dir ./schemas \
  --verb-dir ./verbs \
  --rules-dir ./rules \
  --oci-ref ghcr.io/acme/effectus-payments:1.0.0
```

Resolve the published tag to a digest. Sign that digest with the operator trust system.

Run effectusd with the immutable reference:

```bash
EFFECTUS_POSTGRES_DSN="$POSTGRES_DSN" \
  effectusd \
  --oci-ref ghcr.io/acme/effectus-payments@sha256:BUNDLE_DIGEST \
  --oci-signature-verifier /usr/local/bin/effectus-verify-oci
```

Effectusd does not poll a mutable OCI tag. Deploy a new digest to publish a new generation.

## Archive safety

The shared extractor rejects:

- Absolute paths
- Parent traversal
- Symbolic and hard links
- Device and unsupported entry types
- Excessive file counts
- Excessive file or expanded archive sizes

Nested archives use the same limits.

## Durable execution metadata

A checked invocation carries:

- Stable execution, plan, effect, and dispatch identities
- An idempotency key
- An attempt number
- The contract hash
- A fencing class and token

The destination must enforce idempotency or fencing when correctness requires it.

## Compensation

A verb can name an inverse verb. The runtime records forward success before it considers compensation.

Compensation runs in reverse source order. It is another external operation and can fail.

The runtime blocks automatic compensation when a forward operation has an unknown outcome.

## Security checklist

- Use digest-pinned OCI references.
- Configure and test a signature verifier.
- Keep credentials in a secret store.
- Use TLS and destination authentication.
- Restrict executor network access.
- Set request and response limits.
- Require destination idempotency or fencing where needed.
- Review capability and resource declarations.

## Related documents

- [Checked Compilation Flow](coherent_flow.md)
- [Runtime Guarantees](GUARANTEES.md)
- [Runtime Lifecycle](LIFECYCLE.md)
- [Durable Saga Protocol](DURABLE_SAGA_PROTOCOL.md)
- [Loader Package](https://github.com/josephjohncox/effectus/blob/main/loader/README.md)
