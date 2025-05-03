package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
)

// TypeChecker provides type checking capabilities for the Effectus rule compiler
type TypeChecker struct {
	typeSystem *TypeSystem
}

// NewTypeChecker creates a new TypeChecker with an initialized type system
func NewTypeChecker() *TypeChecker {
	return &TypeChecker{
		typeSystem: NewTypeSystem(),
	}
}

// RegisterProtoTypes loads type information from protobuf definitions
func (tc *TypeChecker) RegisterProtoTypes(protoFile string) error {
	return tc.typeSystem.LoadTypesFromProto(protoFile)
}

// RegisterVerbSpec registers the type information for a verb
func (tc *TypeChecker) RegisterVerbSpec(verb string, argTypes map[string]*Type, returnType *Type) {
	tc.typeSystem.RegisterVerbType(verb, argTypes, returnType)
}

// TypeCheckFile validates a parsed rule file, ensuring all types are correct
func (tc *TypeChecker) TypeCheckFile(file *ast.File, facts effectus.Facts) error {
	// First, infer types from the way facts and variables are used
	if err := tc.typeSystem.InferTypes(file, facts); err != nil {
		return fmt.Errorf("type inference failed: %w", err)
	}

	// Then do a full type check of the file
	return tc.typeSystem.TypeCheckFile(file)
}

// GetFactType returns the inferred or registered type of a fact
func (tc *TypeChecker) GetFactType(path string) (*Type, bool) {
	return tc.typeSystem.GetFactType(path)
}

// MergeTypes merges the types from another type checker
func (tc *TypeChecker) MergeTypes(other *TypeChecker) {
	// Merge fact types
	for path, typ := range other.typeSystem.FactTypes {
		tc.typeSystem.FactTypes[path] = typ
	}

	// Merge verb types
	for verb, info := range other.typeSystem.VerbTypes {
		tc.typeSystem.VerbTypes[verb] = info
	}

	// Merge fact schemas
	for namespace, schema := range other.typeSystem.FactSchemas {
		tc.typeSystem.FactSchemas[namespace] = schema
	}
}

// MergeTypeSystem merges types from a TypeSystem
func (tc *TypeChecker) MergeTypeSystem(system *TypeSystem) {
	// Merge fact types
	for path, typ := range system.FactTypes {
		tc.typeSystem.FactTypes[path] = typ
	}

	// Merge verb types
	for verb, info := range system.VerbTypes {
		tc.typeSystem.VerbTypes[verb] = info
	}

	// Merge fact schemas
	for namespace, schema := range system.FactSchemas {
		tc.typeSystem.FactSchemas[namespace] = schema
	}
}

// GenerateTypeReport generates a human-readable report of inferred types
func (tc *TypeChecker) GenerateTypeReport() string {
	var sb strings.Builder

	sb.WriteString("# Effectus Type Report\n\n")

	// Report fact types
	sb.WriteString("## Fact Types\n\n")
	if len(tc.typeSystem.FactTypes) == 0 {
		sb.WriteString("No fact types inferred or registered.\n\n")
	} else {
		for path, typ := range tc.typeSystem.FactTypes {
			sb.WriteString(fmt.Sprintf("- `%s`: %s\n", path, typ.String()))
		}
		sb.WriteString("\n")
	}

	// Report verb types
	sb.WriteString("## Verb Types\n\n")
	if len(tc.typeSystem.VerbTypes) == 0 {
		sb.WriteString("No verb types registered.\n\n")
	} else {
		for verb, info := range tc.typeSystem.VerbTypes {
			sb.WriteString(fmt.Sprintf("### %s\n", verb))
			sb.WriteString("Arguments:\n")
			for argName, argType := range info.ArgTypes {
				sb.WriteString(fmt.Sprintf("- `%s`: %s\n", argName, argType.String()))
			}
			sb.WriteString(fmt.Sprintf("Return type: %s\n\n", info.ReturnType.String()))
		}
	}

	return sb.String()
}

// BuildTypeSchemaFromFacts examines actual facts to infer their structure
func (tc *TypeChecker) BuildTypeSchemaFromFacts(facts effectus.Facts) {
	// This function would be used to infer types from example facts
	// It would be particularly useful when working with protobuf-defined fact types

	// Just a placeholder implementation for now
	if factMap, ok := facts.(*SimpleFacts); ok {
		for path := range factMap.data {
			val, exists := facts.Get(path)
			if exists {
				tc.typeSystem.InferFactType(path, val)
			}
		}
	}
}

// VerbSpec represents the type specification for a verb
type VerbSpec struct {
	ArgTypes   map[string]Type `json:"arg_types"`
	ReturnType Type            `json:"return_type"`
}

// LoadVerbSpecs loads verb specifications from a JSON file
func (tc *TypeChecker) LoadVerbSpecs(filename string) error {
	// Read the file
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("reading verb spec file: %w", err)
	}

	// Parse the verb specs
	var verbSpecs map[string]VerbSpec
	if err := json.Unmarshal(content, &verbSpecs); err != nil {
		return fmt.Errorf("parsing verb spec file: %w", err)
	}

	// Register each verb
	for verb, spec := range verbSpecs {
		// Convert arg types to proper map
		argTypes := make(map[string]*Type)
		for name, typ := range spec.ArgTypes {
			// Make a copy since we'll be storing pointers
			typCopy := typ
			argTypes[name] = &typCopy
		}

		// Make a copy of the return type
		returnType := spec.ReturnType

		// Register the verb
		tc.RegisterVerbSpec(verb, argTypes, &returnType)
	}

	return nil
}
