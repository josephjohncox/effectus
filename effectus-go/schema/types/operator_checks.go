package types

import (
	"fmt"

	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/pathutil"
)

// Facts interface for type checking
type Facts interface {
	Get(path pathutil.Path) (interface{}, bool)
}

// OperatorCompatibility checks if an operator is compatible with a given type
func (ts *TypeSystem) OperatorCompatibility(operator string, valueType *Type) error {
	switch operator {
	case "==", "!=":
		// Equality operators work with any type
		return nil

	case "<", "<=", ">", ">=":
		// Comparison operators require comparable types
		if !IsComparableType(valueType) {
			return fmt.Errorf("operator %s cannot be used with type %s: type is not comparable",
				operator, valueType.String())
		}
		return nil

	case "contains", "not contains":
		// Contains operators require container types (string, list, map)
		if !IsContainerType(valueType) {
			return fmt.Errorf("operator %s requires a container type (string, list, map), got %s",
				operator, valueType.String())
		}
		return nil

	case "starts with", "ends with":
		// String-specific operators
		if valueType.PrimType != TypeString {
			return fmt.Errorf("operator %s can only be used with string types, got %s",
				operator, valueType.String())
		}
		return nil

	case "in", "not in":
		// 'In' operators require the value to be a list/container
		if valueType.PrimType != TypeList {
			return fmt.Errorf("operator %s requires a list type, got %s",
				operator, valueType.String())
		}
		return nil

	default:
		return fmt.Errorf("unknown operator: %s", operator)
	}
}

// InferTypeFromLiteral infers a Type from an AST Literal
func InferTypeFromLiteral(lit *ast.Literal) *Type {
	if lit == nil {
		return &Type{PrimType: TypeUnknown}
	}

	// Check which literal type is present
	if lit.String != nil {
		return NewStringType()
	}

	if lit.Int != nil {
		return NewIntType()
	}

	if lit.Float != nil {
		return NewFloatType()
	}

	if lit.Bool != nil {
		return NewBoolType()
	}

	if lit.List != nil && len(lit.List) > 0 {
		// Infer element type from first element
		elemType := InferTypeFromLiteral(&lit.List[0])
		return NewListType(elemType)
	} else if lit.List != nil {
		// Empty list - unknown element type
		return NewListType(&Type{PrimType: TypeUnknown})
	}

	if lit.Map != nil && len(lit.Map) > 0 {
		// Create object type and add properties from map entries
		objType := NewObjectType()

		for _, entry := range lit.Map {
			if entry != nil && entry.Key != "" {
				// We only support string keys for now
				propType := InferTypeFromLiteral(&entry.Value)
				objType.AddProperty(entry.Key, propType)
			}
		}

		return objType
	}

	return &Type{PrimType: TypeUnknown}
}

// GetLiteralValue extracts the actual value from a Literal
func GetLiteralValue(lit ast.Literal) interface{} {
	if lit.String != nil {
		return *lit.String
	}

	if lit.Int != nil {
		return *lit.Int
	}

	if lit.Float != nil {
		return *lit.Float
	}

	if lit.Bool != nil {
		return *lit.Bool
	}

	if lit.List != nil {
		// Convert list of literals to slice of values
		values := make([]interface{}, len(lit.List))
		for i, elem := range lit.List {
			values[i] = GetLiteralValue(elem)
		}
		return values
	}

	if lit.Map != nil {
		// Convert map entries to map
		result := make(map[string]interface{})
		for _, entry := range lit.Map {
			if entry != nil {
				result[entry.Key] = GetLiteralValue(entry.Value)
			}
		}
		return result
	}

	return nil
}

// TypeCheckPredicate performs comprehensive type checking on a predicate
func (ts *TypeSystem) TypeCheckPredicate(pred *ast.Predicate) error {
	if pred == nil || pred.PathExpr == nil {
		return fmt.Errorf("invalid predicate: missing path expression")
	}

	// Get the type of the fact at this path
	factType, err := ts.GetFactType(pred.PathExpr.Path)
	if err != nil {
		return fmt.Errorf("unknown fact path in predicate: %s", pred.PathExpr.Path)
	}

	// Check if the operator is compatible with this type
	if err := ts.OperatorCompatibility(pred.Op, factType); err != nil {
		return err
	}

	// Check value compatibility with the fact type
	valueType := InferTypeFromLiteral(&pred.Lit)
	if err := ts.CheckValueTypeCompatibility(pred.Op, factType, valueType); err != nil {
		return fmt.Errorf("value type mismatch in predicate: %w", err)
	}

	return nil
}

// CheckValueTypeCompatibility checks if the value type is compatible with the fact type for the given operator
func (ts *TypeSystem) CheckValueTypeCompatibility(operator string, factType, valueType *Type) error {
	// For equality operators, types should be compatible
	if operator == "==" || operator == "!=" {
		if !AreTypesCompatible(valueType, factType) {
			return fmt.Errorf("incompatible types for %s: %s and %s",
				operator, factType.String(), valueType.String())
		}
		return nil
	}

	// For comparison operators, both types should be comparable with each other
	if operator == "<" || operator == "<=" ||
		operator == ">" || operator == ">=" {
		if !AreComparableTypes(factType, valueType) {
			return fmt.Errorf("types are not comparable for %s: %s and %s",
				operator, factType.String(), valueType.String())
		}
		return nil
	}

	// For contains operators with string
	if (operator == "contains" || operator == "not contains") &&
		factType.PrimType == TypeString {
		if valueType.PrimType != TypeString {
			return fmt.Errorf("string contains operator requires string value, got %s", valueType.String())
		}
		return nil
	}

	// For contains operators with lists
	if (operator == "contains" || operator == "not contains") &&
		factType.PrimType == TypeList {
		if !AreTypesCompatible(valueType, factType.ElementType) {
			return fmt.Errorf("list contains operator requires value compatible with element type %s, got %s",
				factType.ElementType.String(), valueType.String())
		}
		return nil
	}

	// For contains operators with maps
	if (operator == "contains" || operator == "not contains") &&
		factType.PrimType == TypeMap {
		// Check if value is compatible with map keys
		keyType := factType.MapKeyType()
		if !AreTypesCompatible(valueType, keyType) {
			return fmt.Errorf("map contains operator requires value compatible with key type %s, got %s",
				keyType.String(), valueType.String())
		}
		return nil
	}

	// For in/not-in operators
	if operator == "in" || operator == "not in" {
		// Value is a list, check if each element is compatible with fact type
		if valueType.PrimType != TypeList {
			return fmt.Errorf("in operator requires list value, got %s", valueType.String())
		}

		elemType := valueType.ElementType
		if !AreTypesCompatible(factType, elemType) {
			return fmt.Errorf("list elements type %s not compatible with fact type %s for in operator",
				elemType.String(), factType.String())
		}
		return nil
	}

	// For startsWith/endsWith operators
	if operator == "starts with" || operator == "ends with" {
		if factType.PrimType != TypeString || valueType.PrimType != TypeString {
			return fmt.Errorf("string operations require string types, got %s and %s",
				factType.String(), valueType.String())
		}
		return nil
	}

	return fmt.Errorf("unsupported operator for type checking: %s", operator)
}

// IsComparableType returns whether a type supports comparison operators
func IsComparableType(t *Type) bool {
	return t.PrimType == TypeInt ||
		t.PrimType == TypeFloat ||
		t.PrimType == TypeString ||
		t.PrimType == TypeTime ||
		t.PrimType == TypeDate ||
		t.PrimType == TypeDuration
}

// IsContainerType returns whether a type can be used with 'contains' operators
func IsContainerType(t *Type) bool {
	return t.PrimType == TypeString ||
		t.PrimType == TypeList ||
		t.PrimType == TypeMap
}

// AreComparableTypes checks if two types can be compared with operators like <, >, etc.
func AreComparableTypes(t1, t2 *Type) bool {
	// Both must be comparable individually
	if !IsComparableType(t1) || !IsComparableType(t2) {
		return false
	}

	// Numeric types can be compared with each other
	if (t1.PrimType == TypeInt || t1.PrimType == TypeFloat) &&
		(t2.PrimType == TypeInt || t2.PrimType == TypeFloat) {
		return true
	}

	// Time types can be compared with other time types
	if (t1.PrimType == TypeTime || t1.PrimType == TypeDate) &&
		(t2.PrimType == TypeTime || t2.PrimType == TypeDate) {
		return true
	}

	// String can only be compared with string
	if t1.PrimType == TypeString && t2.PrimType == TypeString {
		return true
	}

	// Duration can only be compared with duration
	if t1.PrimType == TypeDuration && t2.PrimType == TypeDuration {
		return true
	}

	return false
}

// TypeCheckLiteralArgument type checks a literal argument value against a required type
func (ts *TypeSystem) TypeCheckLiteralArgument(lit *ast.Literal, requiredType *Type) error {
	if lit == nil {
		return fmt.Errorf("literal value is nil")
	}

	valueType := InferTypeFromLiteral(lit)
	if !AreTypesCompatible(valueType, requiredType) {
		return fmt.Errorf("literal value of type %s not compatible with required type %s",
			valueType.String(), requiredType.String())
	}

	// For list types, check element compatibility
	if lit.List != nil && len(lit.List) > 0 && requiredType.PrimType == TypeList {
		for i, elem := range lit.List {
			elemType := InferTypeFromLiteral(&elem)
			if !AreTypesCompatible(elemType, requiredType.ElementType) {
				return fmt.Errorf("list element %d of type %s not compatible with required element type %s",
					i, elemType.String(), requiredType.ElementType.String())
			}
		}
	}

	// For map types, check property compatibility
	if lit.Map != nil && len(lit.Map) > 0 && requiredType.PrimType == TypeObject {
		for _, entry := range lit.Map {
			if entry == nil || entry.Key == "" {
				return fmt.Errorf("map key must be a string")
			}

			propName := entry.Key
			requiredPropType, exists := requiredType.Properties[propName]
			if !exists {
				// If property doesn't exist in required type
				return fmt.Errorf("unknown property %s in object type", propName)
			}

			propValueType := InferTypeFromLiteral(&entry.Value)
			if !AreTypesCompatible(propValueType, requiredPropType) {
				return fmt.Errorf("property %s of type %s not compatible with required type %s",
					propName, propValueType.String(), requiredPropType.String())
			}
		}
	}

	return nil
}

// TypeCheckArgValue checks if an argument value matches the expected type
func (ts *TypeSystem) TypeCheckArgValue(value *ast.ArgValue, requiredType *Type, facts Facts) error {
	if value == nil {
		return fmt.Errorf("argument value is nil")
	}

	// Handle different value sources
	if value.Literal != nil {
		return ts.TypeCheckLiteralArgument(value.Literal, requiredType)
	}

	if value.PathExpr != nil {
		// For path expressions, first get the fact type, then check compatibility
		factType, err := ts.GetFactType(value.PathExpr.Path)
		if err != nil {
			return fmt.Errorf("unknown fact path: %s", value.PathExpr.Path)
		}

		if !AreTypesCompatible(factType, requiredType) {
			return fmt.Errorf("fact path %s has type %s which is not compatible with required type %s",
				value.PathExpr.Path, factType.String(), requiredType.String())
		}
		return nil
	}

	if value.VarRef != "" {
		// Variable references would be checked against a symbol table
		// This would require context about variables in scope
		return fmt.Errorf("variable reference type checking not implemented")
	}

	return fmt.Errorf("invalid argument value: neither literal, path, nor variable reference")
}

// TypeCheckEffect type checks an entire effect
func (ts *TypeSystem) TypeCheckEffect(effect *ast.Effect, facts Facts) error {
	// Get verb specification
	verbSpec, err := ts.GetVerbSpec(effect.Verb)
	if err != nil {
		return fmt.Errorf("unknown verb: %s", effect.Verb)
	}

	// Check all provided arguments
	for _, arg := range effect.Args {
		// Find the required type for this argument
		requiredType, exists := verbSpec.ArgTypes[arg.Name]
		if !exists {
			return fmt.Errorf("verb %s does not accept argument: %s", effect.Verb, arg.Name)
		}

		// Check if the value matches the required type
		if err := ts.TypeCheckArgValue(arg.Value, requiredType, facts); err != nil {
			return fmt.Errorf("argument %s: %w", arg.Name, err)
		}
	}

	// Check if all required arguments are provided
	for _, requiredArg := range verbSpec.RequiredArgs {
		found := false
		for _, arg := range effect.Args {
			if arg.Name == requiredArg {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("missing required argument %s for verb %s", requiredArg, effect.Verb)
		}
	}

	return nil
}

// Implement similar functions for steps in flows
func (ts *TypeSystem) TypeCheckStep(step *ast.Step, facts Facts) error {
	// Similar implementation as TypeCheckEffect
	verbSpec, err := ts.GetVerbSpec(step.Verb)
	if err != nil {
		return fmt.Errorf("unknown verb: %s", step.Verb)
	}

	// Check all provided arguments
	for _, arg := range step.Args {
		requiredType, exists := verbSpec.ArgTypes[arg.Name]
		if !exists {
			return fmt.Errorf("verb %s does not accept argument: %s", step.Verb, arg.Name)
		}

		if err := ts.TypeCheckArgValue(arg.Value, requiredType, facts); err != nil {
			return fmt.Errorf("argument %s: %w", arg.Name, err)
		}
	}

	// Check required arguments
	for _, requiredArg := range verbSpec.RequiredArgs {
		found := false
		for _, arg := range step.Args {
			if arg.Name == requiredArg {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("missing required argument %s for verb %s", requiredArg, step.Verb)
		}
	}

	return nil
}

// Helper type check functions for list elements and map entries
func (ts *TypeSystem) TypeCheckListElements(elements []ast.Literal, elementType *Type) error {
	for i, element := range elements {
		elemValueType := InferTypeFromLiteral(&element)
		if !AreTypesCompatible(elemValueType, elementType) {
			return fmt.Errorf("list element %d of type %s not compatible with required type %s",
				i, elemValueType.String(), elementType.String())
		}
	}
	return nil
}

func (ts *TypeSystem) TypeCheckMapEntries(entries []*ast.MapEntry, keyType, valueType *Type) error {
	for i, entry := range entries {
		if entry == nil || entry.Key == "" {
			return fmt.Errorf("map entry %d has nil key", i)
		}

		keyValueType := NewStringType()
		if !AreTypesCompatible(keyValueType, keyType) {
			return fmt.Errorf("map entry %d key of type %s not compatible with required key type %s",
				i, keyValueType.String(), keyType.String())
		}

		valueValueType := InferTypeFromLiteral(&entry.Value)
		if !AreTypesCompatible(valueValueType, valueType) {
			return fmt.Errorf("map entry %d value of type %s not compatible with required value type %s",
				i, valueValueType.String(), valueType.String())
		}
	}
	return nil
}

// Helper function to convert operator to string
func operatorToString(op string) string {
	return op
}
