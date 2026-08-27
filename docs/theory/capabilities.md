# Capability Model

Capability metadata describes access, conflict, and retry properties for verbs.

It is not a complete authorization system.

## Declaration

Let a verb contract contain a capability bit set and resource declarations:

```math
v = (name, args, result, capabilities, resources)
```

A resource declaration identifies the protected resource class and required capability bits.

The compiler rejects a resource declaration that exceeds the verb declaration.

## Bit sets, not an authority lattice

Effectus combines capability flags as bit sets. Do not infer a universal order such as read below write below delete.

Different flags describe different properties. For example, exclusive and commutative declarations conflict.

The validator rejects known incompatible combinations.

## Runtime lock selection

The runtime derives a conservative lock requirement from the selected effects.

For one protected resource, it selects the strongest applicable conflict requirement. Equal-priority source order remains stable.

This policy coordinates workers that use the same lock provider and resource key.

## Local advisory locks

A process-local lock protects only that process. Its token is advisory.

It cannot stop another process or an external service from accepting stale work.

## Durable fencing

The PostgreSQL fencing provider issues monotonic tokens for a protected resource.

A destination must compare the supplied token with its last accepted token. It must reject a lower token.

Without that destination check, the token is metadata rather than a fence.

## Static property

A useful static property is declaration containment:

```math
cap(resource) \subseteq cap(verb)
```

The compiler can check this relation because both sets are part of the immutable environment.

## Runtime property

A useful runtime property is stale-completion rejection:

```math
token_{complete} = token_{lease}
```

The durable store rejects completion with a different owner or token.

This property protects Effectus state. It does not undo external work performed by a stale worker.

## Authorization boundary

Capability metadata does not identify a human or service principal. Transport authentication and destination authorization remain separate controls.

A deployment must configure both controls when access policy depends on identity.

## Proof obligations

A full concurrency proof would need to define:

- Resource-key derivation
- Lock compatibility
- Lock acquisition order
- Lease-expiry behavior
- Destination fencing enforcement
- Failure and retry transitions

The current TLA+ saga model covers bounded lease and token transitions. It does not model every destination.
