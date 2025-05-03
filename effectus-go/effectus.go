package effectus

import (
	"context"

	"github.com/effectus/effectus-go/pathutil"
)

// Effect represents a verb and its payload
type Effect struct {
	Verb    string
	Payload interface{}
}

// Executor handles execution of effects
type Executor interface {
	// Do executes an effect and returns a result
	Do(effect Effect) (result interface{}, err error)
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
	Get(path pathutil.Path) (interface{}, bool)

	// Schema returns schema information about the facts
	Schema() SchemaInfo
}

// SchemaInfo provides metadata about the fact schema
type SchemaInfo interface {
	// ValidatePath checks if a path is valid according to the schema
	ValidatePath(path pathutil.Path) bool
}

// Compiler is the interface implemented by both list and flow compilers
type Compiler interface {
	// CompileFile compiles a rule file to a Spec
	CompileFile(path string, schema SchemaInfo) (Spec, error)
}
