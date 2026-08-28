package compiler

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/alecthomas/participle/v2"
	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/schema/types"
)

// Compiler handles parsing and type checking of Effectus files
type Compiler struct {
	parser       *participle.Parser[ast.File]
	typeSystem   *types.TypeSystem
	flowCompiler *flow.Compiler
	listCompiler *list.Compiler
}

// NewCompiler creates a new compiler
func NewCompiler() *Compiler {
	// Create the parser for AST files
	parser, err := participle.Build[ast.File](
		participle.Lexer(ast.Lexer),
		participle.UseLookahead(2),
		participle.Elide("Whitespace", "Comment"),
	)
	if err != nil {
		// If we can't create the parser, panic since this is a fundamental error
		panic(fmt.Errorf("failed to create parser: %w", err))
	}

	return &Compiler{
		parser:       parser,
		typeSystem:   types.NewTypeSystem(),
		flowCompiler: &flow.Compiler{},
		listCompiler: &list.Compiler{},
	}
}

// GetTypeSystem returns the compiler's internal type system
func (c *Compiler) GetTypeSystem() *types.TypeSystem {
	return c.typeSystem
}

// ParseFile parses a file into an AST
func (c *Compiler) ParseFile(filename string) (*ast.File, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	file, err := c.parser.ParseBytes(filename, data)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	if err := normalizeFlowBindings(file); err != nil {
		return nil, err
	}

	return file, nil
}

func normalizeFlowBindings(file *ast.File) error {
	if file == nil {
		return nil
	}
	for _, flow := range file.Flows {
		if flow == nil || flow.Steps == nil {
			continue
		}
		for _, step := range flow.Steps.Steps {
			if step == nil || step.Arrow == "" {
				continue
			}
			if step.BindName != "" {
				return fmt.Errorf("%d:%d: step cannot use both prefix and arrow bindings", step.Pos.Line, step.Pos.Column)
			}
			step.BindName = step.Arrow
			step.Arrow = ""
		}
	}
	return nil
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
	if err := c.typeSystem.TypeCheckFile(file, facts); err != nil {
		return nil, fmt.Errorf("type check error: %w", err)
	}

	return file, nil
}

// CompileFiles parses, type checks, and compiles files through the legacy interface API.
func (c *Compiler) CompileFiles(filenames []string, facts effectus.Facts) (effectus.Spec, error) {
	return c.CompileProgram(filenames, facts)
}

// CompileProgram parses, type checks, and compiles one concrete program.
func (c *Compiler) CompileProgram(filenames []string, facts effectus.Facts) (*CompiledSpec, error) {
	return c.ParseAndCompileProgram(filenames, facts)
}

// CompileUncheckedFiles compiles without type checking.
// Deprecated: production code must use CompileFiles or CompileProgram.
func (c *Compiler) CompileUncheckedFiles(filenames []string, facts effectus.Facts) (effectus.Spec, error) {
	return c.CompileUncheckedProgram(filenames, facts)
}

// CompileUncheckedProgram compiles without type checking.
func (c *Compiler) CompileUncheckedProgram(filenames []string, facts effectus.Facts) (*CompiledSpec, error) {
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
func (c *Compiler) compileAllFiles(effFiles, effxFiles []string, schema effectus.SchemaInfo) (*CompiledSpec, error) {
	var listSpec *list.Spec
	var flowSpec *flow.Spec

	// Compile list-style (.eff) files if any
	if len(effFiles) > 0 {
		var specs []effectus.Spec

		for _, path := range effFiles {
			file, err := c.ParseFile(path)
			if err != nil {
				return nil, fmt.Errorf("failed to parse %s: %w", path, err)
			}

			spec, err := c.listCompiler.CompileParsedFile(file, path, schema)
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
			file, err := c.ParseFile(path)
			if err != nil {
				return nil, fmt.Errorf("failed to parse %s: %w", path, err)
			}

			spec, err := c.flowCompiler.CompileParsedFile(file, path, schema)
			if err != nil {
				return nil, fmt.Errorf("failed to compile %s: %w", path, err)
			}
			specs = append(specs, spec)
		}

		// Merge flow specs
		flowSpec = c.mergeFlowSpecs(specs)
	}

	return &CompiledSpec{List: listSpec, Flow: flowSpec, Name: "unified"}, nil
}

type parsedSource struct {
	path string
	file *ast.File
}

func (c *Compiler) compileParsedSources(sources []parsedSource, schema effectus.SchemaInfo) (*CompiledSpec, error) {
	var listSpecs []effectus.Spec
	var flowSpecs []effectus.Spec
	for _, source := range sources {
		switch filepath.Ext(source.path) {
		case ".eff":
			spec, err := c.listCompiler.CompileParsedFile(source.file, source.path, schema)
			if err != nil {
				return nil, fmt.Errorf("failed to compile %s: %w", source.path, err)
			}
			listSpecs = append(listSpecs, spec)
		case ".effx":
			spec, err := c.flowCompiler.CompileParsedFile(source.file, source.path, schema)
			if err != nil {
				return nil, fmt.Errorf("failed to compile %s: %w", source.path, err)
			}
			flowSpecs = append(flowSpecs, spec)
		default:
			return nil, fmt.Errorf("unsupported file extension for %s", source.path)
		}
	}
	return &CompiledSpec{
		List: c.mergeListSpecs(listSpecs),
		Flow: c.mergeFlowSpecs(flowSpecs),
		Name: "unified",
	}, nil
}

// CompiledSpec is the legacy in-memory list/flow compatibility result. It can
// contain callbacks and must not be serialized as a production artifact. Use
// CompileChecked for validated, callback-free artifacts.
type CompiledSpec struct {
	List *list.Spec
	Flow *flow.Spec
	Name string
}

// NewUnifiedSpec creates a unified spec.
func NewUnifiedSpec(listSpec *list.Spec, flowSpec *flow.Spec, name string) effectus.Spec {
	return &CompiledSpec{List: listSpec, Flow: flowSpec, Name: name}
}

// ListSpec returns the compiled list rules.
func (s *CompiledSpec) ListSpec() *list.Spec {
	return s.List
}

// FlowSpec returns the compiled flow rules.
func (s *CompiledSpec) FlowSpec() *flow.Spec {
	return s.Flow
}

// RequiredFacts implements effectus.Spec
func (s *CompiledSpec) RequiredFacts() []string {
	factPathSet := make(map[string]struct{})

	// Add list spec fact paths
	if s.List != nil {
		for _, path := range s.List.FactPaths {
			factPathSet[path] = struct{}{}
		}
	}

	// Add flow spec fact paths
	if s.Flow != nil {
		for _, path := range s.Flow.FactPaths {
			factPathSet[path] = struct{}{}
		}
	}

	// Extract unique fact paths
	factPaths := make([]string, 0, len(factPathSet))
	for path := range factPathSet {
		factPaths = append(factPaths, path)
	}
	sort.Strings(factPaths)

	return factPaths
}

// GetName implements effectus.Spec
func (s *CompiledSpec) GetName() string {
	return s.Name
}

// Execute implements effectus.Spec
func (s *CompiledSpec) Execute(ctx context.Context, facts effectus.Facts, ex effectus.Executor) error {
	// Execute list spec if available
	if s.List != nil {
		if err := s.List.Execute(ctx, facts, ex); err != nil {
			return fmt.Errorf("list spec execution error: %w", err)
		}
	}

	// Execute flow spec if available
	if s.Flow != nil {
		if err := s.Flow.Execute(ctx, facts, ex); err != nil {
			return fmt.Errorf("flow spec execution error: %w", err)
		}
	}

	return nil
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
	sort.Strings(merged.FactPaths)

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
	sort.Strings(merged.FactPaths)

	return merged
}

// ParseAndCompileFiles parses, checks, and compiles through the legacy API.
// Deprecated: use ParseAndCompileProgram.
func (c *Compiler) ParseAndCompileFiles(filenames []string, facts effectus.Facts) (effectus.Spec, error) {
	return c.ParseAndCompileProgram(filenames, facts)
}

// ParseAndCompileProgram parses, type checks, and compiles one concrete program.
func (c *Compiler) ParseAndCompileProgram(filenames []string, facts effectus.Facts) (*CompiledSpec, error) {
	sources := make([]parsedSource, 0, len(filenames))
	for _, filename := range filenames {
		file, err := c.ParseAndTypeCheck(filename, facts)
		if err != nil {
			return nil, err
		}
		sources = append(sources, parsedSource{path: filename, file: file})
	}
	return c.compileParsedSources(sources, facts.Schema())
}

// LoadVerbSpecs loads verb specifications from a JSON file
func (c *Compiler) LoadVerbSpecs(filename string) error {
	return c.typeSystem.LoadVerbSpecs(filename)
}

// registerDefaultVerbTypes registers basic verb types or loads from file
func (c *Compiler) registerDefaultVerbTypes() error {
	// This method can be simplified to just register the most basic verbs
	// More specific domain verbs should be loaded from schema files

	// SendEmail verb - example of a general utility verb that's always available
	c.typeSystem.RegisterVerbType("SendEmail",
		map[string]*types.Type{
			"to":      {PrimType: types.TypeString},
			"subject": {PrimType: types.TypeString},
			"body":    {PrimType: types.TypeString},
		},
		&types.Type{PrimType: types.TypeBool})

	return nil
}

// RegisterProtoTypes registers types from protobuf files
func (c *Compiler) RegisterProtoTypes(protoFile string) error {
	return c.typeSystem.RegisterProtoTypes(protoFile)
}

// GenerateTypeReport generates a human-readable report of inferred types
func (c *Compiler) GenerateTypeReport() string {
	return c.typeSystem.GenerateTypeReport()
}
