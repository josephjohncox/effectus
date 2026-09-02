# SourceBundle Extension Boundary

Effectus does not have a runtime extension manager. The supported declaration
unit is one immutable `bundle.SourceBundle`.

## Bundle contents

A SourceBundle contains rule sources, an immutable declaration environment, and
verb executor descriptors. `effectusc check`, `compile`, and `inspect` accept
only this bundle. `effectusd` accepts one bundle from a file or a digest-pinned,
signature-verified OCI reference.

The daemon compiles the bundle once at startup. It does not load extension
directories, JSON manifests, plugins, dynamic schemas, or rule updates.

## Executor descriptors

Production daemon bundles use resolved HTTP invocation descriptors. The daemon
resolves every descriptor before it becomes ready. A missing or unsupported
descriptor fails startup.

The HTTP destination receives stable invocation, idempotency, contract, attempt,
and fencing metadata. The destination must enforce idempotency or fencing where
its business mutation needs that guarantee.

The generated gRPC service is an inbound execution API. Kafka is an inbound
fact source. Neither is an outbound executor descriptor.

## Embedded applications

A trusted Go application can use `embedded.Open` with a static resolver
registry. This is process-local application code, not a plugin mechanism and
not a daemon extension path. The shared embedded example consumes the exact
`examples/order_review` rule and data artifacts through Go embedding.

## OCI distribution

OCI distributes the immutable SourceBundle layer. `effectusd --oci-ref` requires
a digest-pinned reference and `--oci-signature-verifier`; the verifier defines
the operator trust policy. OCI tags, unverifiable content, and mutable reload
configuration are not accepted.
