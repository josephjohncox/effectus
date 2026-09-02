package effectus

import (
	"context"
)

// Effect represents a verb and its payload
type Effect struct {
	Verb    string
	Payload interface{}
}

// Executor handles execution of effects
type Executor interface {
	// Do executes an effect without cancellation support.
	// Deprecated: production executors should also implement ContextExecutor. Removal deadline: 2027-09-01.
	Do(effect Effect) (result interface{}, err error)
}

// ContextExecutor executes effects with cancellation and deadline propagation.
type ContextExecutor interface {
	DoContext(ctx context.Context, effect Effect) (result interface{}, err error)
}

// Invoke executes an effect through the context-aware API when available.
func Invoke(ctx context.Context, executor Executor, effect Effect) (interface{}, error) {
	if contextual, ok := executor.(ContextExecutor); ok {
		return InvokeContext(ctx, contextual, effect)
	}
	return executor.Do(effect)
}

// InvokeContext executes an effect using an executor that only implements the
// context-aware contract. It does not require the deprecated Executor.Do method.
func InvokeContext(ctx context.Context, executor ContextExecutor, effect Effect) (interface{}, error) {
	return executor.DoContext(ctx, effect)
}

// Spec is the interface implemented by both list.Spec and flow.Spec
type Spec interface {

	// Name returns the name of the spec
	GetName() string

	// RequiredFacts returns the list of fact paths required by this spec
	RequiredFacts() []string

	// Execute runs the specification with the given facts and executor
	Execute(ctx context.Context, facts Facts, ex Executor) error
}

// Facts represents the structured input data for rules
type Facts interface {
	// Get returns the value at the given path, or nil if not found
	Get(path string) (interface{}, bool)

	// Schema returns schema information about the facts
	Schema() SchemaInfo
}

// SchemaInfo provides metadata about the fact schema
type SchemaInfo interface {
	// ValidatePath checks if a path is valid according to the schema
	ValidatePath(path string) bool
}

// Compiler is the interface implemented by both list and flow compilers
type Compiler interface {
	// CompileFile compiles a rule file to a Spec
	CompileFile(path string, schema SchemaInfo) (Spec, error)
}
