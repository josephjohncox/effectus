package registry

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
)

// ExprFactsAdapter adapts the new expr-based fact system for use with the existing schema registry
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

// CanLoad checks if this loader can handle the given file
func (a *ExprFactsAdapter) CanLoad(path string) bool {
	ext := filepath.Ext(path)
	// Support several formats through our unified loaders
	return ext == ".json" || ext == ".proto" || ext == ".yml" || ext == ".yaml"
}

// Load loads type definitions from a file
func (a *ExprFactsAdapter) Load(path string, typeSystem *types.TypeSystem) error {
	// Choose the appropriate loader based on file extension
	ext := filepath.Ext(path)

	var loader pathutil.FactLoader
	switch ext {
	case ".json":
		// Read file and create JSON loader
		data, err := readFile(path)
		if err != nil {
			return fmt.Errorf("reading JSON file: %w", err)
		}
		loader = pathutil.NewJSONLoader(data)
	case ".proto":
		// For proto files, we need special handling
		// This is a simplified example, real implementation would parse the proto
		return fmt.Errorf("proto loading requires additional setup")
	default:
		return fmt.Errorf("unsupported file extension: %s", ext)
	}

	// Load using our new system
	typedFacts, err := loader.LoadIntoTypedFacts()
	if err != nil {
		return fmt.Errorf("loading into typed facts: %w", err)
	}

	// Register the discovered types with the type system
	// Note: This is a simplified mapping between systems
	// A real implementation would need to map between type representations
	registerTypesFromFacts(typedFacts, typeSystem)

	return nil
}

// CreateFactProvider creates a fact provider from a file
func (a *ExprFactsAdapter) CreateFactProvider(path string) (pathutil.FactProvider, error) {
	// Choose the appropriate loader based on file extension
	ext := filepath.Ext(path)

	var loader pathutil.FactLoader
	switch ext {
	case ".json":
		// Read file and create JSON loader
		data, err := readFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading JSON file: %w", err)
		}
		loader = pathutil.NewJSONLoader(data)
	case ".proto":
		// For proto files, we need special handling
		return nil, fmt.Errorf("proto loading requires additional setup")
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}

	// Load into typed facts
	typedFacts, err := loader.LoadIntoTypedFacts()
	if err != nil {
		return nil, fmt.Errorf("loading into typed facts: %w", err)
	}

	return typedFacts, nil
}

// Helper functions

// readFile reads a file into a byte slice
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
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
			// This is a simplified approach - in a real implementation
			// you would recursively determine the element type
			elemType := types.NewAnyType()

			// Try to get the type of the first element if available
			value, ok := facts.Get(path)
			if ok {
				if arr, ok := value.([]interface{}); ok && len(arr) > 0 {
					elemPath := fmt.Sprintf("%s[0]", path)
					elemTypeInfo := facts.GetTypeInfo(elemPath)
					elemType = convertTypeNameToType(elemTypeInfo)
				}
			}

			typ = types.NewListType(elemType)
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
