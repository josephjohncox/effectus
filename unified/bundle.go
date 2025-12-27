// unified/bundle.go
package unified

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	eff "github.com/effectus/effectus-go"
	comp "github.com/effectus/effectus-go/compiler"
	fl "github.com/effectus/effectus-go/flow"
	list "github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
)

// Bundle represents a compiled bundle of rules and schemas
type Bundle struct {
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	Description   string             `json:"description"`
	VerbHash      string             `json:"verb_hash"`
	CreatedAt     time.Time          `json:"created_at"`
	SchemaFiles   []string           `json:"schema_files"`
	VerbFiles     []string           `json:"verb_files"`
	RuleFiles     []string           `json:"rule_files"`
	ListSpec      *list.Spec         `json:"-"`
	FlowSpec      *fl.Spec           `json:"-"`
	Rules         []RuleSummary      `json:"rules,omitempty"`
	Flows         []FlowSummary      `json:"flows,omitempty"`
	FactTypes     []FactTypeSummary  `json:"fact_types,omitempty"`
	VerbSpecs     []VerbSpecSummary  `json:"verb_specs,omitempty"`
	RequiredFacts []string           `json:"required_facts"`
	PIIMasks      []string           `json:"pii_masks,omitempty"`
}

// FactTypeSummary describes a fact path and its inferred type.
type FactTypeSummary struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

// VerbSpecSummary captures the verb signature metadata for UI/status.
type VerbSpecSummary struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	Capability   string            `json:"capability,omitempty"`
	InverseVerb  string            `json:"inverse,omitempty"`
	ArgTypes     map[string]string `json:"arg_types,omitempty"`
	RequiredArgs []string          `json:"required_args,omitempty"`
	ReturnType   string            `json:"return_type,omitempty"`
}

// RuleSummary summarizes a compiled list rule.
type RuleSummary struct {
	Name       string              `json:"name"`
	Priority   int                 `json:"priority"`
	Predicates []string            `json:"predicates,omitempty"`
	FactPaths  []string            `json:"fact_paths,omitempty"`
	Effects    []RuleEffectSummary `json:"effects,omitempty"`
}

// RuleEffectSummary summarizes a verb effect in a rule.
type RuleEffectSummary struct {
	Verb string                 `json:"verb"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// FlowSummary summarizes a compiled flow.
type FlowSummary struct {
	Name       string   `json:"name"`
	Priority   int      `json:"priority"`
	Predicates []string `json:"predicates,omitempty"`
	FactPaths  []string `json:"fact_paths,omitempty"`
	Verbs      []string `json:"verbs,omitempty"`
}

// BundleBuilder helps construct a bundle
type BundleBuilder struct {
	bundle          *Bundle
	schemaDir       string
	schemaPaths     []string
	verbSpecPaths   []string
	verbDir         string
	rulesDir        string
	schemaRegistry  *schema.Registry
	verbRegistry    *verb.Registry
	compiler        *comp.Compiler
	skipCompilation bool // Flag to skip rule compilation
}

// NewBundleBuilder creates a new bundle builder
func NewBundleBuilder(name, version string) *BundleBuilder {
	return &BundleBuilder{
		bundle: &Bundle{
			Name:      name,
			Version:   version,
			CreatedAt: time.Now(),
		},
		schemaRegistry: schema.NewRegistry(),
	}
}

// RegisterSchemaLoader registers a loader for schema files
func (bb *BundleBuilder) RegisterSchemaLoader(loader interface{}) *BundleBuilder {
	// This method is now a no-op since RegisterLoader is not available
	// Consider implementing a different approach for schema loading
	return bb
}

// WithSchemaDir specifies the directory for schema files
func (bb *BundleBuilder) WithSchemaDir(dir string) *BundleBuilder {
	bb.schemaDir = dir
	return bb
}

// WithVerbSpecFiles registers verb spec schema files for type checking.
func (bb *BundleBuilder) WithVerbSpecFiles(paths []string) *BundleBuilder {
	bb.verbSpecPaths = append(bb.verbSpecPaths, paths...)
	return bb
}

// WithVerbDir specifies the directory for verb files
func (bb *BundleBuilder) WithVerbDir(dir string) *BundleBuilder {
	bb.verbDir = dir
	return bb
}

// WithRulesDir specifies the directory for rule files
func (bb *BundleBuilder) WithRulesDir(dir string) *BundleBuilder {
	bb.rulesDir = dir
	return bb
}

// WithDescription sets the bundle description
func (bb *BundleBuilder) WithDescription(desc string) *BundleBuilder {
	bb.bundle.Description = desc
	return bb
}

// WithPIIMasks sets the PII masking paths
func (bb *BundleBuilder) WithPIIMasks(masks []string) *BundleBuilder {
	bb.bundle.PIIMasks = masks
	return bb
}

// SkipRuleCompilation skips the compilation of rules
// Useful for testing when rule compilation is not the focus
func (bb *BundleBuilder) SkipRuleCompilation() *BundleBuilder {
	bb.skipCompilation = true
	return bb
}

// Build builds the bundle from the specified directories
func (bb *BundleBuilder) Build() (*Bundle, error) {
	// Load schema files
	if bb.schemaDir != "" {
		if err := bb.loadSchemas(); err != nil {
			return nil, fmt.Errorf("loading schemas: %w", err)
		}
	}

	// Create a type system
	typeSystem := types.NewTypeSystem()
	if err := bb.loadSchemasIntoTypeSystem(typeSystem); err != nil {
		return nil, fmt.Errorf("loading schema types: %w", err)
	}
	if err := bb.loadVerbSpecsIntoTypeSystem(typeSystem); err != nil {
		return nil, fmt.Errorf("loading verb specs: %w", err)
	}

	// Capture schema + verb summaries for status/UI.
	bb.bundle.FactTypes = SummarizeFactTypes(typeSystem)
	bb.bundle.VerbSpecs = SummarizeVerbSpecs(typeSystem)

	// Create verb registry with proper type system
	bb.verbRegistry = verb.NewRegistry(typeSystem)

	// Load verbs
	if bb.verbDir != "" {
		if err := bb.loadVerbs(); err != nil {
			return nil, fmt.Errorf("loading verbs: %w", err)
		}
	}

	// Save verb hash
	bb.bundle.VerbHash = bb.verbRegistry.GetVerbHash()

	// Create compiler
	bb.compiler = comp.NewCompiler()

	// Use our type system and verb registry
	typeSystem2 := bb.compiler.GetTypeSystem()
	if typeSystem2 != nil {
		typeSystem2.MergeTypeSystem(typeSystem)
	}

	// Load and compile rules
	if bb.rulesDir != "" {
		if err := bb.loadRules(); err != nil {
			return nil, fmt.Errorf("loading rules: %w", err)
		}
	}

	return bb.bundle, nil
}

// loadSchemas loads schema files from the schema directory
func (bb *BundleBuilder) loadSchemas() error {
	return filepath.Walk(bb.schemaDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		ext := filepath.Ext(path)
		if ext != ".json" && ext != ".proto" {
			return nil
		}

		relPath, err := filepath.Rel(bb.schemaDir, path)
		if err != nil {
			return fmt.Errorf("getting relative path: %w", err)
		}

		// Record in bundle
		bb.bundle.SchemaFiles = append(bb.bundle.SchemaFiles, relPath)
		bb.schemaPaths = append(bb.schemaPaths, path)

		return nil
	})
}

func (bb *BundleBuilder) loadSchemasIntoTypeSystem(typeSystem *types.TypeSystem) error {
	if typeSystem == nil || len(bb.schemaPaths) == 0 {
		return nil
	}
	for _, path := range bb.schemaPaths {
		switch filepath.Ext(path) {
		case ".json":
			if err := typeSystem.LoadSchemaFile(path); err != nil {
				if jsonErr := typeSystem.LoadJSONSchemaFile(path); jsonErr != nil {
					return fmt.Errorf("loading schema file %s: %w", path, err)
				}
			}
		case ".proto":
			if err := typeSystem.RegisterProtoTypes(path); err != nil {
				return fmt.Errorf("loading proto schema %s: %w", path, err)
			}
		}
	}
	return nil
}

func (bb *BundleBuilder) loadVerbSpecsIntoTypeSystem(typeSystem *types.TypeSystem) error {
	if typeSystem == nil || len(bb.verbSpecPaths) == 0 {
		return nil
	}
	for _, path := range bb.verbSpecPaths {
		if filepath.Ext(path) != ".json" {
			continue
		}
		if err := typeSystem.LoadVerbSpecs(path); err != nil {
			return fmt.Errorf("loading verb specs from %s: %w", path, err)
		}
	}
	return nil
}

// loadVerbs loads verb files from the verb directory
func (bb *BundleBuilder) loadVerbs() error {
	return filepath.Walk(bb.verbDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		ext := filepath.Ext(path)
		if ext != ".json" {
			return nil
		}

		relPath, err := filepath.Rel(bb.verbDir, path)
		if err != nil {
			return fmt.Errorf("getting relative path: %w", err)
		}

		// Record in bundle
		bb.bundle.VerbFiles = append(bb.bundle.VerbFiles, relPath)

		// Load into registry
		if err := bb.verbRegistry.LoadFromJSON(path); err != nil {
			return fmt.Errorf("loading verbs from %s: %w", relPath, err)
		}

		return nil
	})
}

// loadRules loads and compiles rules from the rules directory
func (bb *BundleBuilder) loadRules() error {
	// Find all rule files
	listRuleFiles := []string{}
	flowRuleFiles := []string{}

	err := filepath.Walk(bb.rulesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		ext := filepath.Ext(path)
		relPath, err := filepath.Rel(bb.rulesDir, path)
		if err != nil {
			return fmt.Errorf("getting relative path: %w", err)
		}

		// Record in bundle
		bb.bundle.RuleFiles = append(bb.bundle.RuleFiles, relPath)

		// Categorize by extension
		if ext == ".eff" {
			listRuleFiles = append(listRuleFiles, path)
		} else if ext == ".effx" {
			flowRuleFiles = append(flowRuleFiles, path)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("walking rules directory: %w", err)
	}

	// Skip compilation if requested
	if bb.skipCompilation {
		return nil
	}

	// Create empty facts for compilation
	facts := bb.createEmptyFacts()

	// Compile all rules
	compiledSpec, err := bb.compiler.ParseAndCompileFiles(append(listRuleFiles, flowRuleFiles...), facts)
	if err != nil {
		return fmt.Errorf("compiling rules: %w", err)
	}

	// Extract list and flow specs (in-memory only) and summarize for storage.
	bb.bundle.ListSpec = getListSpec(compiledSpec)
	bb.bundle.FlowSpec = getFlowSpec(compiledSpec)
	bb.bundle.Rules = SummarizeRules(bb.bundle.ListSpec)
	bb.bundle.Flows = SummarizeFlows(bb.bundle.FlowSpec)
	bb.bundle.RequiredFacts = compiledSpec.RequiredFacts()

	return nil
}

// getListSpec extracts a list.Spec from an effectus.Spec using reflection
func getListSpec(spec eff.Spec) *list.Spec {
	// For now, just attempt a direct type check and conversion
	// This is a placeholder for a more robust solution
	type specWithListField interface {
		GetName() string
		RequiredFacts() []string
		ListSpec() *list.Spec
	}

	if s, ok := spec.(specWithListField); ok {
		return s.ListSpec()
	}

	return nil
}

// getFlowSpec extracts a flow.Spec from an effectus.Spec using reflection
func getFlowSpec(spec eff.Spec) *fl.Spec {
	// For now, just attempt a direct type check and conversion
	// This is a placeholder for a more robust solution
	type specWithFlowField interface {
		GetName() string
		RequiredFacts() []string
		FlowSpec() *fl.Spec
	}

	if s, ok := spec.(specWithFlowField); ok {
		return s.FlowSpec()
	}

	return nil
}

// createEmptyFacts creates a dummy facts implementation for compilation
func (bb *BundleBuilder) createEmptyFacts() eff.Facts {
	return &testFacts{
		factRegistry: pathutil.NewRegistry(),
		schema:       &testSchema{},
	}
}

// SummarizeFactTypes returns a stable summary of registered fact paths and types.
func SummarizeFactTypes(ts *types.TypeSystem) []FactTypeSummary {
	if ts == nil {
		return nil
	}
	paths := ts.GetAllFactPaths()
	sort.Strings(paths)
	summaries := make([]FactTypeSummary, 0, len(paths))
	for _, path := range paths {
		typ, err := ts.GetFactType(path)
		typeName := "unknown"
		if err == nil && typ != nil {
			typeName = typ.String()
		}
		summaries = append(summaries, FactTypeSummary{
			Path: path,
			Type: typeName,
		})
	}
	return summaries
}

// SummarizeVerbSpecs returns a stable summary of registered verb specs.
func SummarizeVerbSpecs(ts *types.TypeSystem) []VerbSpecSummary {
	if ts == nil {
		return nil
	}
	names := ts.GetAllVerbNames()
	sort.Strings(names)
	summaries := make([]VerbSpecSummary, 0, len(names))
	for _, name := range names {
		spec, err := ts.GetVerbSpec(name)
		if err != nil || spec == nil {
			continue
		}
		argTypes := make(map[string]string, len(spec.ArgTypes))
		for argName, argType := range spec.ArgTypes {
			if argType == nil {
				argTypes[argName] = "unknown"
				continue
			}
			argTypes[argName] = argType.String()
		}
		returnType := "unknown"
		if spec.ReturnType != nil {
			returnType = spec.ReturnType.String()
		}
		summaries = append(summaries, VerbSpecSummary{
			Name:         spec.Name,
			Description:  spec.Description,
			Capability:   spec.Capability.String(),
			InverseVerb:  spec.InverseVerb,
			ArgTypes:     argTypes,
			RequiredArgs: append([]string(nil), spec.RequiredArgs...),
			ReturnType:   returnType,
		})
	}
	return summaries
}

// SummarizeRules returns a summary of list rules suitable for JSON export.
func SummarizeRules(spec *list.Spec) []RuleSummary {
	if spec == nil {
		return nil
	}
	summaries := make([]RuleSummary, 0, len(spec.Rules))
	for _, rule := range spec.Rules {
		if rule == nil {
			continue
		}
		predicates := make([]string, 0, len(rule.Predicates))
		for _, predicate := range rule.Predicates {
			if predicate == nil {
				continue
			}
			predicates = append(predicates, predicate.Expression)
		}
		effects := make([]RuleEffectSummary, 0, len(rule.Effects))
		for _, effect := range rule.Effects {
			if effect == nil {
				continue
			}
			effects = append(effects, RuleEffectSummary{
				Verb: effect.Verb,
				Args: effect.Args,
			})
		}
		summaries = append(summaries, RuleSummary{
			Name:       rule.Name,
			Priority:   rule.Priority,
			Predicates: predicates,
			FactPaths:  append([]string(nil), rule.FactPaths...),
			Effects:    effects,
		})
	}
	return summaries
}

// SummarizeFlows returns a summary of flows suitable for JSON export.
func SummarizeFlows(spec *fl.Spec) []FlowSummary {
	if spec == nil {
		return nil
	}
	summaries := make([]FlowSummary, 0, len(spec.Flows))
	for _, flowSpec := range spec.Flows {
		if flowSpec == nil {
			continue
		}
		predicates := make([]string, 0, len(flowSpec.Predicates))
		for _, predicate := range flowSpec.Predicates {
			if predicate == nil {
				continue
			}
			predicates = append(predicates, predicate.Expression)
		}
		verbs := collectProgramVerbs(flowSpec.Program)
		summaries = append(summaries, FlowSummary{
			Name:       flowSpec.Name,
			Priority:   flowSpec.Priority,
			Predicates: predicates,
			FactPaths:  append([]string(nil), flowSpec.FactPaths...),
			Verbs:      verbs,
		})
	}
	return summaries
}

func collectProgramVerbs(program *fl.Program) []string {
	verbCounts := make(map[string]int)
	collectProgramVerbsRecursive(program, verbCounts)
	verbs := make([]string, 0, len(verbCounts))
	for verb := range verbCounts {
		verbs = append(verbs, verb)
	}
	sort.Strings(verbs)
	return verbs
}

func collectProgramVerbsRecursive(program *fl.Program, verbCounts map[string]int) {
	if program == nil {
		return
	}
	switch program.Tag {
	case fl.EffectProgramTag:
		if program.Effect.Verb != "" {
			verbCounts[program.Effect.Verb]++
		}
		if program.Continue != nil {
			next := program.Continue(nil)
			collectProgramVerbsRecursive(next, verbCounts)
		}
	case fl.TransactionProgramTag:
		if program.Transaction != nil {
			collectProgramVerbsRecursive(program.Transaction.Program, verbCounts)
		}
		if program.Continue != nil {
			next := program.Continue(nil)
			collectProgramVerbsRecursive(next, verbCounts)
		}
	case fl.PureProgramTag:
		return
	}
}

// testSchema is a simple schema implementation for testing
type testSchema struct{}

// ValidatePath implements the SchemaInfo interface
func (s *testSchema) ValidatePath(path string) bool {
	// Simple implementation that accepts all paths
	return true
}

// testFacts is a simple facts implementation for testing
type testFacts struct {
	factRegistry *pathutil.Registry
	schema       *testSchema
}

// Get implements the Facts interface
func (f *testFacts) Get(path string) (interface{}, bool) {
	return f.factRegistry.Get(path)
}

// Schema implements the Facts interface
func (f *testFacts) Schema() eff.SchemaInfo {
	return f.schema
}

// SaveBundle saves a bundle to disk
func SaveBundle(bundle *Bundle, filePath string) error {
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling bundle: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("writing bundle: %w", err)
	}

	return nil
}

// LoadBundle loads a bundle from disk
func LoadBundle(filePath string) (*Bundle, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading bundle: %w", err)
	}

	var bundle Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("unmarshaling bundle: %w", err)
	}

	return &bundle, nil
}
