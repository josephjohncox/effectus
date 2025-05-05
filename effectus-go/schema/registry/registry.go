package registry

import (
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/types"
)

// ExprRegistry provides centralized management of schemas using the expr-based implementation
type ExprRegistry struct {
	// typeSystem manages type definitions
	typeSystem *types.TypeSystem

	// schema is the expr-based schema implementation
	schema *schema.ExprSchema
}

// NewExprRegistry creates a new schema registry
func NewExprRegistry(typeSystem *types.TypeSystem) *ExprRegistry {
	if typeSystem == nil {
		typeSystem = types.NewTypeSystem()
	}

	// Create a default namespace
	return &ExprRegistry{
		typeSystem: typeSystem,
		schema:     schema.NewExprSchema(""),
	}
}

// GetTypeSystem returns the underlying type system
func (r *ExprRegistry) GetTypeSystem() *types.TypeSystem {
	return r.typeSystem
}

// GetSchema returns the underlying ExprSchema
func (r *ExprRegistry) GetSchema() *schema.ExprSchema {
	return r.schema
}

// LoadSchema loads a schema from a file
func (r *ExprRegistry) LoadSchema(path string) error {
	return r.schema.LoadFromFile(path)
}

// LoadDirectory loads all schema files from a directory
func (r *ExprRegistry) LoadDirectory(dir string) error {
	return r.schema.LoadFromDirectory(dir)
}

// Get retrieves a value from the facts
func (r *ExprRegistry) Get(path string) (interface{}, bool) {
	return r.schema.Get(path)
}

// GetWithType retrieves a value and its type information
func (r *ExprRegistry) GetWithType(path string) (interface{}, string, bool) {
	return r.schema.GetWithType(path)
}

// EvaluateExpr evaluates an expression using the facts
func (r *ExprRegistry) EvaluateExpr(expr string) (interface{}, error) {
	return r.schema.EvaluateExpr(expr)
}

// EvaluateExprBool evaluates an expression that returns a boolean
func (r *ExprRegistry) EvaluateExprBool(expr string) (bool, error) {
	return r.schema.EvaluateExprBool(expr)
}

// TypeCheckExpr type-checks an expression
func (r *ExprRegistry) TypeCheckExpr(expr string) error {
	return r.schema.TypeCheckExpr(expr)
}

// GetFactProvider returns the schema as a FactProvider
func (r *ExprRegistry) GetFactProvider() pathutil.FactProvider {
	return r.schema.AsFactProvider()
}

// RegisterType registers a type for a path
func (r *ExprRegistry) RegisterType(path string, typeName string) {
	r.schema.RegisterType(path, typeName)
}

// Backwards compatibility functions for transitional purposes

// SchemaRegistry is maintained for backward compatibility
type SchemaRegistry struct {
	*ExprRegistry
}

// NewSchemaRegistry creates a compatibility wrapper around ExprRegistry
func NewSchemaRegistry(typeSystem *types.TypeSystem) *SchemaRegistry {
	return &SchemaRegistry{
		ExprRegistry: NewExprRegistry(typeSystem),
	}
}
