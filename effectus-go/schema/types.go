package schema

import (
	"errors"
	"fmt"
	"strings"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
)

// Type represents a data type in the Effectus type system
type Type struct {
	Name       string
	PrimType   PrimitiveType
	MapKeyType *Type
	MapValType *Type
	ListType   *Type
}

// PrimitiveType represents the fundamental types
type PrimitiveType int

const (
	TypeUnknown PrimitiveType = iota
	TypeString
	TypeInt
	TypeFloat
	TypeBool
	TypeList
	TypeMap
)

// String returns a string representation of the type
func (t *Type) String() string {
	switch t.PrimType {
	case TypeString:
		return "string"
	case TypeInt:
		return "int"
	case TypeFloat:
		return "float"
	case TypeBool:
		return "bool"
	case TypeList:
		if t.ListType != nil {
			return fmt.Sprintf("[]%s", t.ListType.String())
		}
		return "[]any"
	case TypeMap:
		if t.MapKeyType != nil && t.MapValType != nil {
			return fmt.Sprintf("map[%s]%s", t.MapKeyType.String(), t.MapValType.String())
		}
		return "map[string]any"
	default:
		if t.Name != "" {
			return t.Name
		}
		return "unknown"
	}
}

// TypeSystem manages the types for facts and variables
type TypeSystem struct {
	FactTypes   map[string]*Type        // Maps fact paths to their types
	VarTypes    map[string]*Type        // Maps variable names to their types
	VerbTypes   map[string]VerbTypeInfo // Maps verbs to their argument and return types
	FactSchemas map[string]FactSchema   // Maps fact namespaces to their schemas
}

// VerbTypeInfo contains type information for a verb
type VerbTypeInfo struct {
	ArgTypes   map[string]*Type // Maps argument names to their types
	ReturnType *Type            // Return type of the verb
}

// FactSchema represents the schema for a fact namespace
type FactSchema struct {
	Fields map[string]*Type      // Maps field names to their types
	Nested map[string]FactSchema // Maps nested field names to their schemas
}

// NewTypeSystem creates a new type system
func NewTypeSystem() *TypeSystem {
	return &TypeSystem{
		FactTypes:   make(map[string]*Type),
		VarTypes:    make(map[string]*Type),
		VerbTypes:   make(map[string]VerbTypeInfo),
		FactSchemas: make(map[string]FactSchema),
	}
}

// RegisterFactType registers a type for a fact path
func (ts *TypeSystem) RegisterFactType(path string, typ *Type) {
	ts.FactTypes[path] = typ

	// Also update the fact schema
	parts := strings.Split(path, ".")
	if len(parts) > 1 {
		namespace := parts[0]
		if _, exists := ts.FactSchemas[namespace]; !exists {
			ts.FactSchemas[namespace] = FactSchema{
				Fields: make(map[string]*Type),
				Nested: make(map[string]FactSchema),
			}
		}

		schema := ts.FactSchemas[namespace]
		for i := 1; i < len(parts)-1; i++ {
			fieldName := parts[i]
			if _, exists := schema.Nested[fieldName]; !exists {
				schema.Nested[fieldName] = FactSchema{
					Fields: make(map[string]*Type),
					Nested: make(map[string]FactSchema),
				}
			}
			schema = schema.Nested[fieldName]
		}

		lastField := parts[len(parts)-1]
		schema.Fields[lastField] = typ
	}
}

// RegisterVerbType registers type information for a verb
func (ts *TypeSystem) RegisterVerbType(verb string, argTypes map[string]*Type, returnType *Type) {
	ts.VerbTypes[verb] = VerbTypeInfo{
		ArgTypes:   argTypes,
		ReturnType: returnType,
	}
}

// InferFactType infers the type of a fact based on its usage
func (ts *TypeSystem) InferFactType(path string, value interface{}) *Type {
	if typ, exists := ts.FactTypes[path]; exists {
		return typ
	}

	// Infer type from value
	var inferredType *Type
	switch v := value.(type) {
	case string:
		inferredType = &Type{PrimType: TypeString}
	case int:
		inferredType = &Type{PrimType: TypeInt}
	case float64:
		inferredType = &Type{PrimType: TypeFloat}
	case bool:
		inferredType = &Type{PrimType: TypeBool}
	case []interface{}:
		if len(v) > 0 {
			elemType := ts.InferFactType(path+"[0]", v[0])
			inferredType = &Type{
				PrimType: TypeList,
				ListType: elemType,
			}
		} else {
			inferredType = &Type{
				PrimType: TypeList,
				ListType: &Type{PrimType: TypeUnknown},
			}
		}
	case map[string]interface{}:
		inferredType = &Type{
			PrimType:   TypeMap,
			MapKeyType: &Type{PrimType: TypeString},
			MapValType: &Type{PrimType: TypeUnknown},
		}
		// Try to infer map value type from first entry
		for _, val := range v {
			inferredType.MapValType = ts.InferFactType(path+".value", val)
			break
		}
	default:
		inferredType = &Type{PrimType: TypeUnknown}
	}

	ts.RegisterFactType(path, inferredType)
	return inferredType
}

// GetFactType gets the type of a fact path
func (ts *TypeSystem) GetFactType(path string) (*Type, bool) {
	typ, exists := ts.FactTypes[path]
	return typ, exists
}

// TypeCheckPredicate checks if a predicate's comparison is type-safe
func (ts *TypeSystem) TypeCheckPredicate(pred *ast.Predicate) error {
	factType, exists := ts.GetFactType(pred.Path)
	if !exists {
		return fmt.Errorf("unknown fact type for path: %s", pred.Path)
	}

	var literalType *Type
	if pred.Lit.String != nil {
		literalType = &Type{PrimType: TypeString}
	} else if pred.Lit.Int != nil {
		literalType = &Type{PrimType: TypeInt}
	} else if pred.Lit.Float != nil {
		literalType = &Type{PrimType: TypeFloat}
	} else if pred.Lit.Bool != nil {
		literalType = &Type{PrimType: TypeBool}
	} else if pred.Lit.List != nil {
		literalType = &Type{PrimType: TypeList}
	} else if pred.Lit.Map != nil {
		literalType = &Type{PrimType: TypeMap}
	}

	// Check if the comparison makes sense for these types
	switch pred.Op {
	case "==", "!=":
		// These operators work on any comparable types
		return nil
	case "<", "<=", ">", ">=":
		// These operators only work on numeric types
		if factType.PrimType != TypeInt && factType.PrimType != TypeFloat {
			return fmt.Errorf("operator %s cannot be applied to %s", pred.Op, factType.String())
		}
		if literalType.PrimType != TypeInt && literalType.PrimType != TypeFloat {
			return fmt.Errorf("operator %s cannot be applied to %s", pred.Op, literalType.String())
		}
	case "in":
		// 'in' requires the literal to be a list or map
		if literalType.PrimType != TypeList && literalType.PrimType != TypeMap {
			return fmt.Errorf("operator 'in' requires a list or map on the right side")
		}
	case "contains":
		// 'contains' requires the fact to be a list, map, or string
		if factType.PrimType != TypeList && factType.PrimType != TypeMap && factType.PrimType != TypeString {
			return fmt.Errorf("operator 'contains' requires a list, map, or string on the left side")
		}
	}

	return nil
}

// TypeCheckEffect checks if an effect's arguments match the expected types
func (ts *TypeSystem) TypeCheckEffect(effect *ast.Effect, bindings map[string]*Type) error {
	verbInfo, exists := ts.VerbTypes[effect.Verb]
	if !exists {
		return fmt.Errorf("unknown verb: %s", effect.Verb)
	}

	// Check each argument
	for _, arg := range effect.Args {
		expectedType, argExists := verbInfo.ArgTypes[arg.Name]
		if !argExists {
			return fmt.Errorf("unknown argument %s for verb %s", arg.Name, effect.Verb)
		}

		if arg.Value == nil {
			return fmt.Errorf("argument %s has no value", arg.Name)
		}

		var argType *Type

		if arg.Value.VarRef != "" {
			// Check variable reference
			varName := strings.TrimPrefix(arg.Value.VarRef, "$")
			varType, varExists := ts.VarTypes[varName]
			if !varExists {
				if bindings != nil {
					varType, varExists = bindings[varName]
				}
				if !varExists {
					return fmt.Errorf("undefined variable: %s", varName)
				}
			}
			argType = varType
		} else if arg.Value.FactPath != "" {
			// Check fact path
			factType, factExists := ts.GetFactType(arg.Value.FactPath)
			if !factExists {
				return fmt.Errorf("unknown fact type for path: %s", arg.Value.FactPath)
			}
			argType = factType
		} else if arg.Value.Literal != nil {
			// Check literal
			if arg.Value.Literal.String != nil {
				argType = &Type{PrimType: TypeString}
			} else if arg.Value.Literal.Int != nil {
				argType = &Type{PrimType: TypeInt}
			} else if arg.Value.Literal.Float != nil {
				argType = &Type{PrimType: TypeFloat}
			} else if arg.Value.Literal.Bool != nil {
				argType = &Type{PrimType: TypeBool}
			} else if arg.Value.Literal.List != nil {
				argType = &Type{PrimType: TypeList}
			} else if arg.Value.Literal.Map != nil {
				argType = &Type{PrimType: TypeMap}
			}
		}

		// Now check if argType is compatible with expectedType
		if !isTypeCompatible(argType, expectedType) {
			return fmt.Errorf("type mismatch for argument %s: expected %s, got %s",
				arg.Name, expectedType.String(), argType.String())
		}
	}

	return nil
}

// isTypeCompatible checks if the provided type is compatible with the expected type
func isTypeCompatible(provided, expected *Type) bool {
	// Same primitive type is always compatible
	if provided.PrimType == expected.PrimType {
		// For complex types, check inner types
		if provided.PrimType == TypeList {
			if provided.ListType == nil || expected.ListType == nil {
				return true
			}
			return isTypeCompatible(provided.ListType, expected.ListType)
		}
		if provided.PrimType == TypeMap {
			if provided.MapKeyType == nil || expected.MapKeyType == nil ||
				provided.MapValType == nil || expected.MapValType == nil {
				return true
			}
			return isTypeCompatible(provided.MapKeyType, expected.MapKeyType) &&
				isTypeCompatible(provided.MapValType, expected.MapValType)
		}
		return true
	}

	// Numeric types can be compatible with each other
	if (provided.PrimType == TypeInt || provided.PrimType == TypeFloat) &&
		(expected.PrimType == TypeInt || expected.PrimType == TypeFloat) {
		return true
	}

	// Unknown type is compatible with anything (for gradual typing)
	if provided.PrimType == TypeUnknown || expected.PrimType == TypeUnknown {
		return true
	}

	return false
}

// TypeCheckRule performs type checking on a rule
func (ts *TypeSystem) TypeCheckRule(rule *ast.Rule) error {
	// Check predicates
	if rule.When != nil {
		for _, pred := range rule.When.Predicates {
			if err := ts.TypeCheckPredicate(pred); err != nil {
				return fmt.Errorf("in rule %s, when: %w", rule.Name, err)
			}
		}
	}

	// Check effects
	if rule.Then != nil {
		for _, effect := range rule.Then.Effects {
			if err := ts.TypeCheckEffect(effect, nil); err != nil {
				return fmt.Errorf("in rule %s, then: %w", rule.Name, err)
			}
		}
	}

	return nil
}

// TypeCheckFlow performs type checking on a flow
func (ts *TypeSystem) TypeCheckFlow(flow *ast.Flow) error {
	// Check predicates
	if flow.When != nil {
		for _, pred := range flow.When.Predicates {
			if err := ts.TypeCheckPredicate(pred); err != nil {
				return fmt.Errorf("in flow %s, when: %w", flow.Name, err)
			}
		}
	}

	// Track variable bindings throughout the flow
	bindings := make(map[string]*Type)

	// Check steps
	if flow.Steps != nil {
		for _, step := range flow.Steps.Steps {
			// Check if the verb exists
			verbInfo, exists := ts.VerbTypes[step.Verb]
			if !exists {
				return fmt.Errorf("in flow %s, unknown verb: %s", flow.Name, step.Verb)
			}

			// Check each argument
			if err := ts.TypeCheckEffect(&ast.Effect{
				Verb: step.Verb,
				Args: step.Args,
			}, bindings); err != nil {
				return fmt.Errorf("in flow %s, steps: %w", flow.Name, err)
			}

			// If this step binds a variable, update the bindings
			if step.BindName != "" {
				bindings[step.BindName] = verbInfo.ReturnType
			}
		}
	}

	return nil
}

// TypeCheckFile performs type checking on a file containing rules and flows
func (ts *TypeSystem) TypeCheckFile(file *ast.File) error {
	for _, rule := range file.Rules {
		if err := ts.TypeCheckRule(rule); err != nil {
			return err
		}
	}

	for _, flow := range file.Flows {
		if err := ts.TypeCheckFlow(flow); err != nil {
			return err
		}
	}

	return nil
}

// LoadTypesFromProto loads type information from protobuf definitions
// This is a placeholder for future implementation when protobuf is available
func (ts *TypeSystem) LoadTypesFromProto(protoFile string) error {
	// This would be implemented when proto definitions are available
	return errors.New("not yet implemented")
}

// InferTypes infers types from the usage of facts and variables in rules and flows
func (ts *TypeSystem) InferTypes(file *ast.File, facts effectus.Facts) error {
	// Infer fact types from predicates
	for _, rule := range file.Rules {
		if rule.When != nil {
			for _, pred := range rule.When.Predicates {
				factValue, exists := facts.Get(pred.Path)
				if exists {
					ts.InferFactType(pred.Path, factValue)
				}
			}
		}
	}

	for _, flow := range file.Flows {
		if flow.When != nil {
			for _, pred := range flow.When.Predicates {
				factValue, exists := facts.Get(pred.Path)
				if exists {
					ts.InferFactType(pred.Path, factValue)
				}
			}
		}
	}

	// After inferring types, perform type checking
	return ts.TypeCheckFile(file)
}
