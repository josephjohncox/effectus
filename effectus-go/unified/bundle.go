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
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/schema"
)

// Bundle represents a complete effectus bundle
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

// BundleBuilder assists in creating bundles
type BundleBuilder struct {
	bundle         *Bundle
	schemaDir      string
	verbDir        string
	rulesDir       string
	schemaRegistry *schema.SchemaRegistry
	verbRegistry   *schema.VerbRegistry
	compiler       *comp.Compiler
}

// NewBundleBuilder creates a new bundle builder
func NewBundleBuilder(name, version string) *BundleBuilder {
	return &BundleBuilder{
		bundle: &Bundle{
			Name:          name,
			Version:       version,
			CreatedAt:     time.Now(),
			SchemaFiles:   []string{},
			VerbFiles:     []string{},
			RuleFiles:     []string{},
			RequiredFacts: []string{},
			PIIMasks:      []string{},
		},
		schemaRegistry: schema.NewSchemaRegistry(),
	}
}

// WithSchemaDir sets the schema directory
func (bb *BundleBuilder) WithSchemaDir(dir string) *BundleBuilder {
	bb.schemaDir = dir
	return bb
}

// WithVerbDir sets the verb directory
func (bb *BundleBuilder) WithVerbDir(dir string) *BundleBuilder {
	bb.verbDir = dir
	return bb
}

// WithRulesDir sets the rules directory
func (bb *BundleBuilder) WithRulesDir(dir string) *BundleBuilder {
	bb.rulesDir = dir
	return bb
}

// WithDescription sets the bundle description
func (bb *BundleBuilder) WithDescription(desc string) *BundleBuilder {
	bb.bundle.Description = desc
	return bb
}

// WithPIIMasks sets PII mask paths
func (bb *BundleBuilder) WithPIIMasks(masks []string) *BundleBuilder {
	bb.bundle.PIIMasks = masks
	return bb
}

// Build creates the complete bundle
func (bb *BundleBuilder) Build() (*Bundle, error) {
	// Load schemas
	if bb.schemaDir != "" {
		if err := bb.loadSchemas(); err != nil {
			return nil, fmt.Errorf("loading schemas: %w", err)
		}
	}

	// Create type system from schema registry
	typeSystem := bb.schemaRegistry.GetTypeSystem()

	// Create verb registry
	bb.verbRegistry = schema.NewVerbRegistry(typeSystem)

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
	if setter, ok := bb.compiler.(interface{ SetTypeSystem(*schema.TypeSystem) }); ok {
		setter.SetTypeSystem(typeSystem)
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

		// Load into registry
		if ext == ".json" {
			if err := bb.schemaRegistry.LoadJSONSchema(path); err != nil {
				return fmt.Errorf("loading JSON schema %s: %w", relPath, err)
			}
		} else if ext == ".proto" {
			if err := bb.schemaRegistry.LoadProtoSchema(path); err != nil {
				return fmt.Errorf("loading proto schema %s: %w", relPath, err)
			}
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
		if err := bb.verbRegistry.LoadVerbsFromJSON(path); err != nil {
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

	// Create empty facts for compilation
	facts := bb.createEmptyFacts()

	// Compile all rules
	spec, err := bb.compiler.ParseAndCompileFiles(append(listRuleFiles, flowRuleFiles...), facts)
	if err != nil {
		return fmt.Errorf("compiling rules: %w", err)
	}

	// Extract list and flow specs
	if unifiedSpec, ok := spec.(*Spec); ok {
		bb.bundle.ListSpec = unifiedSpec.ListSpec
		bb.bundle.FlowSpec = unifiedSpec.FlowSpec
	}

	// Record required facts
	bb.bundle.RequiredFacts = spec.RequiredFacts()

	return nil
}

// createEmptyFacts creates an empty facts object for compilation
func (bb *BundleBuilder) createEmptyFacts() eff.Facts {
	// Create a simple schema info using SimpleSchema
	schemaInfo := &schema.SimpleSchema{}

	// Create empty facts
	return schema.NewSimpleFacts(map[string]interface{}{}, schemaInfo)
}

// SaveBundle saves the bundle to disk
func SaveBundle(bundle *Bundle, filePath string) error {
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling bundle: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("writing bundle file: %w", err)
	}

	return nil
}

// LoadBundle loads a bundle from disk
func LoadBundle(filePath string) (*Bundle, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading bundle file: %w", err)
	}

	var bundle Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("unmarshaling bundle: %w", err)
	}

	return &bundle, nil
}
