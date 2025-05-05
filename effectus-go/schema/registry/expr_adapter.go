package registry

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
)

// ExprFactsAdapter provides adapters between schema types and expr types
type ExprFactsAdapter struct {
	// The associated type system
	typeSystem *types.TypeSystem
}

// NewExprFactsAdapter creates a new adapter
func NewExprFactsAdapter(typeSystem *types.TypeSystem) *ExprFactsAdapter {
	return &ExprFactsAdapter{
		typeSystem: typeSystem,
	}
}

// CreateFactProvider creates a fact provider from a file
func (a *ExprFactsAdapter) CreateFactProvider(path string) (pathutil.FactProvider, error) {
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	// Choose the appropriate loader based on file extension
	ext := filepath.Ext(path)
	switch ext {
	case ".json":
		// Create a loader for JSON data
		loader := pathutil.NewJSONLoader(data)
		typedFacts, err := loader.LoadIntoTypedFacts()
		if err != nil {
			return nil, fmt.Errorf("loading into typed facts: %w", err)
		}

		// Register discovered types if needed
		if a.typeSystem != nil {
			registerTypesFromFacts(typedFacts, a.typeSystem)
		}

		return typedFacts, nil
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}
}

// registerTypesFromFacts registers types discovered in facts with the type system
func registerTypesFromFacts(facts *pathutil.TypedExprFacts, typeSystem *types.TypeSystem) {
	// Get all paths with a prefix (empty string gets all paths)
	paths := facts.GetPathsByPrefix("")

	// Register each path with its type
	for _, path := range paths {
		// Get the type info
		typeInfo := facts.GetTypeInfo(path)

		// Convert expr type name to TypeSystem type
		var typ *types.Type
		switch typeInfo {
		case "boolean":
			typ = types.NewBoolType()
		case "integer":
			typ = types.NewIntType()
		case "float":
			typ = types.NewFloatType()
		case "string":
			typ = types.NewStringType()
		case "array":
			// For arrays, we need the element type
			// Check the first element if available
			value, ok := facts.Get(path)
			if ok {
				if arr, ok := value.([]interface{}); ok && len(arr) > 0 {
					elemPath := fmt.Sprintf("%s[0]", path)
					elemTypeInfo := facts.GetTypeInfo(elemPath)
					elemType := convertTypeNameToType(elemTypeInfo)
					typ = types.NewListType(elemType)
				} else {
					// Empty array or not an array
					typ = types.NewListType(types.NewAnyType())
				}
			} else {
				typ = types.NewListType(types.NewAnyType())
			}
		case "map", "object":
			// For objects, create an object type
			objType := types.NewObjectType()

			// Try to get property types if available
			value, ok := facts.Get(path)
			if ok {
				if obj, ok := value.(map[string]interface{}); ok {
					for propName := range obj {
						propPath := fmt.Sprintf("%s.%s", path, propName)
						propTypeInfo := facts.GetTypeInfo(propPath)
						propType := convertTypeNameToType(propTypeInfo)
						objType.AddProperty(propName, propType)
					}
				}
			}

			typ = objType
		default:
			// For unknown types, use Any type
			typ = types.NewAnyType()
		}

		// Register this fact type
		typeSystem.RegisterFactType(path, typ)
	}
}

// Helper to convert a type name to a Type
func convertTypeNameToType(typeName string) *types.Type {
	switch typeName {
	case "boolean":
		return types.NewBoolType()
	case "integer":
		return types.NewIntType()
	case "float":
		return types.NewFloatType()
	case "string":
		return types.NewStringType()
	case "array":
		// Default to list of Any
		return types.NewListType(types.NewAnyType())
	case "map", "object":
		return types.NewObjectType()
	default:
		return types.NewAnyType()
	}
}
