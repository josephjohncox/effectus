// unified/bundle.go
package unified

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Name          string     `json:"name"`
	Version       string     `json:"version"`
	Description   string     `json:"description"`
	VerbHash      string     `json:"verb_hash"`
	CreatedAt     time.Time  `json:"created_at"`
	SchemaFiles   []string   `json:"schema_files"`
	VerbFiles     []string   `json:"verb_files"`
	RuleFiles     []string   `json:"rule_files"`
	ListSpec      *list.Spec `json:"list_spec,omitempty"`
	FlowSpec      *fl.Spec   `json:"flow_spec,omitempty"`
	RequiredFacts []string   `json:"required_facts"`
	PIIMasks      []string   `json:"pii_masks,omitempty"`
}

// BundleBuilder helps construct a bundle
type BundleBuilder struct {
	bundle          *Bundle
	schemaDir       string
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
		// Merge type systems if needed - this depends on your implementation
		// typeSystem2.MergeTypeSystem(typeSystem)
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

		// LoadFile is no longer available, so we need a different approach
		// This is a simplified version
		if ext == ".json" {
			// Record that we would have loaded this file
			fmt.Printf("Would load JSON schema file: %s\n", path)
		} else if ext == ".proto" {
			// Record that we would have loaded this file
			fmt.Printf("Would load Proto schema file: %s\n", path)
		}

		return nil
	})
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

	// Extract list and flow specs
	bb.bundle.ListSpec = getListSpec(compiledSpec)
	bb.bundle.FlowSpec = getFlowSpec(compiledSpec)
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
