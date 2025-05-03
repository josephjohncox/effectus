package compiler

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/alecthomas/participle/v2"
	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/unified"
)

// Compiler handles parsing and type checking of Effectus files
type Compiler struct {
	parser       *participle.Parser[ast.File]
	typeChecker  *schema.TypeChecker
	flowCompiler *flow.Compiler
	listCompiler *list.Compiler
}

// NewCompiler creates a new compiler
func NewCompiler() *Compiler {
	return &Compiler{
		parser:       effectus.GetParser(),
		typeChecker:  schema.NewTypeChecker(),
		flowCompiler: &flow.Compiler{},
		listCompiler: &list.Compiler{},
	}
}

// GetTypeChecker returns the compiler's internal type checker
func (c *Compiler) GetTypeChecker() *schema.TypeChecker {
	return c.typeChecker
}

// ParseFile parses a file into an AST
func (c *Compiler) ParseFile(filename string) (*ast.File, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	file, err := c.parser.ParseBytes(filename, data)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return file, nil
}

// ParseAndTypeCheck parses a file and performs type checking
func (c *Compiler) ParseAndTypeCheck(filename string, facts effectus.Facts) (*ast.File, error) {
	// Parse the file first
	file, err := c.ParseFile(filename)
	if err != nil {
		return nil, err
	}

	// Make sure we have registered default verb types
	if err := c.registerDefaultVerbTypes(); err != nil {
		return nil, fmt.Errorf("failed to register verb types: %w", err)
	}

	// Perform type checking
	if err := c.typeChecker.TypeCheckFile(file, facts); err != nil {
		return nil, fmt.Errorf("type check error: %w", err)
	}

	return file, nil
}

// CompileFiles compiles multiple rule files into a unified spec
func (c *Compiler) CompileFiles(filenames []string, facts effectus.Facts) (effectus.Spec, error) {
	// Group files by extension
	effFiles := []string{}
	effxFiles := []string{}

	for _, path := range filenames {
		ext := filepath.Ext(path)
		switch ext {
		case ".eff":
			effFiles = append(effFiles, path)
		case ".effx":
			effxFiles = append(effxFiles, path)
		default:
			return nil, fmt.Errorf("unsupported file extension for %s: %s (must be .eff or .effx)", path, ext)
		}
	}

	// Create a schema
	schema := facts.Schema()

	// Compile all files and merge them
	return c.compileAllFiles(effFiles, effxFiles, schema)
}

// compileAllFiles compiles both list and flow style rule files and merges them into a single spec
func (c *Compiler) compileAllFiles(effFiles, effxFiles []string, schema effectus.SchemaInfo) (effectus.Spec, error) {
	var listSpec *list.Spec
	var flowSpec *flow.Spec

	// Compile list-style (.eff) files if any
	if len(effFiles) > 0 {
		var specs []effectus.Spec

		for _, path := range effFiles {
			spec, err := c.listCompiler.CompileFile(path, schema)
			if err != nil {
				return nil, fmt.Errorf("failed to compile %s: %w", path, err)
			}
			specs = append(specs, spec)
		}

		// Merge list specs
		listSpec = c.mergeListSpecs(specs)
	}

	// Compile flow-style (.effx) files if any
	if len(effxFiles) > 0 {
		var specs []effectus.Spec
		for _, path := range effxFiles {
			spec, err := c.flowCompiler.CompileFile(path, schema)
			if err != nil {
				return nil, fmt.Errorf("failed to compile %s: %w", path, err)
			}
			specs = append(specs, spec)
		}

		// Merge flow specs
		flowSpec = c.mergeFlowSpecs(specs)
	}

	// Create unified spec that combines both types
	unifiedSpec := &unified.Spec{
		ListSpec: listSpec,
		FlowSpec: flowSpec,
		Name:     "unified",
	}

	return unifiedSpec, nil
}

// mergeListSpecs merges multiple list specs into a single one
func (c *Compiler) mergeListSpecs(specs []effectus.Spec) *list.Spec {
	if len(specs) == 0 {
		return nil
	}

	merged := &list.Spec{
		Rules:     []*list.CompiledRule{},
		FactPaths: []string{},
	}

	factPathSet := make(map[string]struct{})

	for _, spec := range specs {
		listSpec, ok := spec.(*list.Spec)
		if !ok {
			continue
		}

		// Add rules
		merged.Rules = append(merged.Rules, listSpec.Rules...)

		// Collect fact paths
		for _, path := range listSpec.FactPaths {
			factPathSet[path] = struct{}{}
		}
	}

	// Extract unique fact paths
	for path := range factPathSet {
		merged.FactPaths = append(merged.FactPaths, path)
	}

	return merged
}

// mergeFlowSpecs merges multiple flow specs into a single one
func (c *Compiler) mergeFlowSpecs(specs []effectus.Spec) *flow.Spec {
	if len(specs) == 0 {
		return nil
	}

	merged := &flow.Spec{
		Flows:     []*flow.CompiledFlow{},
		FactPaths: []string{},
	}

	factPathSet := make(map[string]struct{})

	for _, spec := range specs {
		flowSpec, ok := spec.(*flow.Spec)
		if !ok {
			continue
		}

		// Add flows
		merged.Flows = append(merged.Flows, flowSpec.Flows...)

		// Collect fact paths
		for _, path := range flowSpec.FactPaths {
			factPathSet[path] = struct{}{}
		}
	}

	// Extract unique fact paths
	for path := range factPathSet {
		merged.FactPaths = append(merged.FactPaths, path)
	}

	return merged
}

// ParseAndCompileFiles parses, type checks, and compiles multiple files
func (c *Compiler) ParseAndCompileFiles(filenames []string, facts effectus.Facts) (effectus.Spec, error) {
	// Type check all files
	for _, filename := range filenames {
		_, err := c.ParseAndTypeCheck(filename, facts)
		if err != nil {
			return nil, err
		}
	}

	// Compile all files into a unified spec
	return c.CompileFiles(filenames, facts)
}

// LoadVerbSpecs loads verb specifications from a JSON file
func (c *Compiler) LoadVerbSpecs(filename string) error {
	return c.typeChecker.LoadVerbSpecs(filename)
}

// registerDefaultVerbTypes registers basic verb types or loads from file
func (c *Compiler) registerDefaultVerbTypes() error {
	// This method can be simplified to just register the most basic verbs
	// More specific domain verbs should be loaded from schema files

	// SendEmail verb - example of a general utility verb that's always available
	c.typeChecker.RegisterVerbSpec("SendEmail",
		map[string]*schema.Type{
			"to":      {PrimType: schema.TypeString},
			"subject": {PrimType: schema.TypeString},
			"body":    {PrimType: schema.TypeString},
		},
		&schema.Type{PrimType: schema.TypeBool})

	return nil
}

// RegisterProtoTypes registers types from protobuf files
func (c *Compiler) RegisterProtoTypes(protoFile string) error {
	return c.typeChecker.RegisterProtoTypes(protoFile)
}

// GenerateTypeReport generates a human-readable report of inferred types
func (c *Compiler) GenerateTypeReport() string {
	return c.typeChecker.GenerateTypeReport()
}
