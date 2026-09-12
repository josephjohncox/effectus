// Package runtime executes checked artifacts through immutable generations.
// Applications that need the default in-memory setup should start with package embedded.
//
// # Construction and ownership
//
// CompileGeneration builds executable identity from a source bundle and descriptor resolvers.
// NewEngine takes generation ownership on success. ConfigureWorkflow and ConfigureLedger
// must run before execution when the application needs non-default stores.
// NewEngine does not start background recovery or transport servers.
//
// Engine.Generation returns a borrowed generation. ArtifactResolver instead transfers
// ownership of each returned generation, including a generation returned with an error.
// Do not return another engine's borrowed generation from an artifact resolver.
//
// # Execution and errors
//
// WaitAccepted acknowledges durable admission, not business completion.
// WaitTerminal returns unsuccessful terminal states through TerminalExecutionError.
// Use errors.As and errors.Is rather than parsing error text.
// Underlying causes can contain sensitive data and must not reach untrusted clients.
//
// # Shutdown
//
// Stop admission and recovery workers before closing the engine.
// Engine.Close drains entered calls and closes owned generations.
// It leaves borrowed stores, database handles, and fencing providers open.
// Close those dependencies after the engine finishes.
// Cancellation is cooperative and cannot force a business operation to stop.
// Never call Engine.Close from an executor running on that engine.
package runtime
