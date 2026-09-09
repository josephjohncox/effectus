// Package schema provides execution stores, the outbox dispatcher, identity helpers,
// migration helpers, and retained compatibility types.
//
// New applications should enter through package embedded or runtime.
// Custom durable stores implement the contracts in schema/ledger and schema/workflow.
// Aliases retained here have the same contracts, not separate persistence semantics.
// Store revisions, leases, and fencing checks must remain atomic.
//
// In-memory stores do not persist across process restart.
// PostgreSQL stores borrow their database handles. The caller owns database shutdown.
// Stop workers and drain engine calls before closing a shared database.
// An unknown destination outcome is not evidence that retry is safe.
//
// BufIntegration and its schema metadata are local compatibility support.
// They do not implement the runtime's schema registry, privacy enforcement,
// retention enforcement, or capability authorization.
// Prefer checked-in protobuf definitions and explicit Buf CLI steps for new builds.
package schema
