# Proto-Driven Development

This example shows how a team can use protobuf files as the source for fact and verb data types.

The directory is a template. It does not include the example `.proto` files or generated Go packages.

## Included files

```text
company_example/
├── buf.yaml
├── buf.gen.yaml
└── service_implementation.go.example
```

`buf.yaml` defines a Buf v2 module named `buf.build/acme/effectus-schemas`.

`buf.gen.yaml` generates Go code with `protoc-gen-go` through the Buf remote plugin.

The service template has the `proto_demo` build tag. It compiles only after you add schemas and generated packages.

## Create the schema tree

```bash
cd examples/proto_driven_development/company_example
mkdir -p proto/acme/v1/facts proto/acme/v1/verbs
```

Add your fact messages under `proto/acme/v1/facts`. Add verb input and result messages under `proto/acme/v1/verbs`.

Use stable package and field names. Reserve removed field numbers and names.

## Example verb messages

```protobuf
syntax = "proto3";

package acme.v1.verbs;

option go_package = "github.com/effectus/examples/proto_driven_development/company_example/gen/go/acme/v1/verbs;verbsv1";

message SendNotificationInput {
  string user_id = 1;
  string message = 2;
  NotificationType type = 3;
}

message SendNotificationOutput {
  string notification_id = 1;
  DeliveryStatus status = 2;
  bool success = 3;
}

enum NotificationType {
  NOTIFICATION_TYPE_UNSPECIFIED = 0;
  NOTIFICATION_TYPE_EMAIL = 1;
  NOTIFICATION_TYPE_SMS = 2;
  NOTIFICATION_TYPE_PUSH = 3;
}

enum DeliveryStatus {
  DELIVERY_STATUS_UNSPECIFIED = 0;
  DELIVERY_STATUS_QUEUED = 1;
  DELIVERY_STATUS_SENT = 2;
  DELIVERY_STATUS_FAILED = 3;
}
```

## Validate and generate

Install Buf, then run:

```bash
buf format -w
buf lint
buf build
buf generate
```

Generated Go files appear under `gen/go` with source-relative paths.

The current generator config produces Go only. Add reviewed Buf plugins when another language needs generated types.

## Check compatibility

Compare the module with its configured baseline:

```bash
buf breaking --against '.git#branch=main'
```

Choose a baseline that exists in your repository or registry workflow.

Buf compatibility checks schema shape. They do not prove application-level compatibility or migration safety.

## Adapt the service template

After generation:

1. Update the generated import paths in `service_implementation.go.example`.
2. Replace provider credentials with secret-backed configuration.
3. Implement the provider interfaces.
4. Add strict input and output validation.
5. Rename the file only when it belongs in a buildable module.

The template shows a typed service method and a `verb.Registry` contract. The adapter between map arguments and protobuf messages must validate every conversion.

## Connect the contract to Effectus

A protobuf message defines a data type. Effectus also needs a verb contract that declares:

- The verb name
- Required arguments
- Argument and result types
- Capabilities and resources
- The executor target
- An optional inverse verb

Production effectusd uses declarative HTTP, gRPC, stream, Kafka, or OCI-resolved targets. It rejects in-process plugins.

A trusted embedded application can register a static Go executor. This path remains outside the checked daemon boundary.

## Versioning rules

- Do not reuse a field number.
- Reserve removed field numbers and names.
- Add enum values without changing existing numbers.
- Treat a package-version change as a new contract family.
- Run `buf lint`, `buf build`, and `buf breaking` in CI.
- Recompile Effectus rules after a relevant schema or verb change.

## Security rules

Generated types do not validate business authorization, idempotency, or fencing.

The destination must authenticate callers and enforce invocation metadata when correctness requires it.

Do not commit provider credentials or generated test secrets.

## Related documents

- [Basics](../../docs/BASICS.md)
- [Extension System](../../docs/EXTENSION_SYSTEM.md)
- [Checked Compilation Flow](../../docs/coherent_flow.md)
- [Runtime Guarantees](../../docs/GUARANTEES.md)
