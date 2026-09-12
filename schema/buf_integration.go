package schema

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// BufIntegration is a process-local compatibility wrapper for trusted Buf workspaces.
// Its methods serialize mutations and return owned schema snapshots.
// New applications should use checked-in protobuf definitions and explicit Buf CLI commands.
// This wrapper is not the Engine's schema registry or a general JSON Schema compiler.
type BufIntegration struct {
	workspaceRoot string
	protoDir      string

	// Schema registries
	verbRegistry *VerbSchemaRegistry
	factRegistry *FactSchemaRegistry

	// Code generation tracking
	generationMutex sync.RWMutex
	lastGeneration  time.Time

	// Buf configuration
	bufConfig *BufConfig
}

// BufConfig represents the buf.yaml configuration
type BufConfig struct {
	Version  string            `yaml:"version"`
	Name     string            `yaml:"name"`
	Deps     []string          `yaml:"deps"`
	Breaking BufBreakingConfig `yaml:"breaking"`
	Lint     BufLintConfig     `yaml:"lint"`
	Build    BufBuildConfig    `yaml:"build"`
}

// BufBreakingConfig configures breaking change detection
type BufBreakingConfig struct {
	Use []string `yaml:"use"`
}

// BufLintConfig configures linting
type BufLintConfig struct {
	Use                 []string `yaml:"use"`
	Except              []string `yaml:"except"`
	AllowCommentIgnores bool     `yaml:"allow_comment_ignores"`
}

// BufBuildConfig configures build settings
type BufBuildConfig struct {
	Excludes []string `yaml:"excludes"`
}

// BufGenConfig represents the buf.gen.yaml configuration
type BufGenConfig struct {
	Version string            `yaml:"version"`
	Managed BufManagedConfig  `yaml:"managed"`
	Plugins []BufPluginConfig `yaml:"plugins"`
}

// BufManagedConfig configures managed mode
type BufManagedConfig struct {
	Enabled         bool                     `yaml:"enabled"`
	GoPackagePrefix BufGoPackagePrefixConfig `yaml:"go_package_prefix"`
}

// BufGoPackagePrefixConfig configures Go package prefixes
type BufGoPackagePrefixConfig struct {
	Default string   `yaml:"default"`
	Except  []string `yaml:"except"`
}

// BufPluginConfig configures a code generation plugin
type BufPluginConfig struct {
	Plugin string   `yaml:"plugin"`
	Out    string   `yaml:"out"`
	Opt    []string `yaml:"opt"`
}

// VerbSchemaRegistry manages verb interface schemas
type VerbSchemaRegistry struct {
	schemas map[string]*VerbSchema
	mutex   sync.RWMutex
}

// VerbSchema represents a versioned verb interface schema
type VerbSchema struct {
	Name                 string                 `json:"name"`
	Version              string                 `json:"version"`
	Description          string                 `json:"description"`
	InputSchema          map[string]interface{} `json:"input_schema"`
	OutputSchema         map[string]interface{} `json:"output_schema"`
	RequiredCapabilities []string               `json:"required_capabilities"`
	ExecutionType        string                 `json:"execution_type"`
	Idempotent           bool                   `json:"idempotent"`
	Compensatable        bool                   `json:"compensatable"`
	BufModule            string                 `json:"buf_module"`
	BufCommit            string                 `json:"buf_commit"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

// FactSchemaRegistry manages fact schemas
type FactSchemaRegistry struct {
	schemas map[string]*FactSchema
	mutex   sync.RWMutex
}

// FactSchema represents a versioned fact schema
type FactSchema struct {
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	Description     string                 `json:"description"`
	Schema          map[string]interface{} `json:"schema"`
	Indexes         []IndexDefinition      `json:"indexes"`
	RetentionPolicy *RetentionPolicy       `json:"retention_policy"`
	PrivacyRules    []PrivacyRule          `json:"privacy_rules"`
	BufModule       string                 `json:"buf_module"`
	BufCommit       string                 `json:"buf_commit"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// IndexDefinition defines an index on fact data
type IndexDefinition struct {
	Name    string            `json:"name"`
	Fields  []string          `json:"fields"`
	Type    string            `json:"type"`
	Unique  bool              `json:"unique"`
	Sparse  bool              `json:"sparse"`
	Options map[string]string `json:"options"`
}

// RetentionPolicy defines data retention rules
type RetentionPolicy struct {
	Duration   string            `json:"duration"`
	Strategy   string            `json:"strategy"`
	Conditions map[string]string `json:"conditions"`
}

// PrivacyRule defines privacy and masking rules
type PrivacyRule struct {
	FieldPath    string            `json:"field_path"`
	Action       string            `json:"action"`
	MaskPattern  string            `json:"mask_pattern"`
	AllowedRoles []string          `json:"allowed_roles"`
	Conditions   map[string]string `json:"conditions"`
}

// SchemaValidationResult represents the result of schema validation
type SchemaValidationResult struct {
	Valid           bool     `json:"valid"`
	Errors          []string `json:"errors"`
	Warnings        []string `json:"warnings"`
	BreakingChanges []string `json:"breaking_changes"`
	Suggestions     []string `json:"suggestions"`
}

// CodeGenerationResult represents the result of code generation
type CodeGenerationResult struct {
	Success        bool                   `json:"success"`
	GeneratedFiles []string               `json:"generated_files"`
	Errors         []string               `json:"errors"`
	Warnings       []string               `json:"warnings"`
	Duration       time.Duration          `json:"duration"`
	Metadata       map[string]interface{} `json:"metadata"`
}

// NewBufIntegration reads local configuration without creating directories or files.
func NewBufIntegration(workspaceRoot string) (*BufIntegration, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, fmt.Errorf("workspace root is required")
	}
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	workspaceRoot = root
	protoDir := filepath.Join(workspaceRoot, "proto")

	integration := &BufIntegration{
		workspaceRoot: workspaceRoot,
		protoDir:      protoDir,
		verbRegistry:  &VerbSchemaRegistry{schemas: make(map[string]*VerbSchema)},
		factRegistry:  &FactSchemaRegistry{schemas: make(map[string]*FactSchema)},
	}

	// Load existing configuration
	if err := integration.loadBufConfig(); err != nil {
		return nil, fmt.Errorf("failed to load buf config: %w", err)
	}

	return integration, nil
}

// loadBufConfig loads the buf.yaml configuration
func (b *BufIntegration) loadBufConfig() error {
	configPath := filepath.Join(b.workspaceRoot, "buf.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Keep the legacy proto/ location without inventing a disk configuration.
			b.bufConfig = &BufConfig{Version: "v1"}
			return nil
		}
		return fmt.Errorf("failed to read buf config: %w", err)
	}

	if err := yaml.Unmarshal(data, &b.bufConfig); err != nil {
		return fmt.Errorf("failed to parse buf config: %w", err)
	}
	if b.bufConfig == nil || (b.bufConfig.Version != "v1" && b.bufConfig.Version != "v2") {
		return fmt.Errorf("Buf configuration version must be v1 or v2")
	}
	if b.bufConfig.Version == "v2" {
		var config struct {
			Modules []struct {
				Path string `yaml:"path"`
			} `yaml:"modules"`
		}
		if err := yaml.Unmarshal(data, &config); err != nil {
			return err
		}
		if len(config.Modules) > 1 {
			return fmt.Errorf("legacy BufIntegration requires one module. Use the Buf CLI for multiple modules")
		}
		if len(config.Modules) == 1 {
			path := config.Modules[0].Path
			if path == "" || filepath.IsAbs(path) {
				return fmt.Errorf("Buf module path must be relative to the workspace")
			}
			path = filepath.Clean(path)
			if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
				return fmt.Errorf("Buf module path must remain within the workspace")
			}
			b.protoDir = filepath.Join(b.workspaceRoot, path)
		}
	}
	return nil
}

// RegisterVerbSchema copies metadata and installs a new scalar protobuf definition.
// Existing definitions must be identical. Caller inputs must remain stable during the call.
func (b *BufIntegration) RegisterVerbSchema(ctx context.Context, schema *VerbSchema) error {
	if err := b.checkContext(ctx); err != nil {
		return err
	}
	if schema == nil {
		return fmt.Errorf("verb schema is required")
	}
	owned, err := cloneBufVerb(schema)
	if err != nil {
		return fmt.Errorf("copy verb schema: %w", err)
	}
	schema = owned
	if err := checkBufSchemaNames(schema.Name, schema.InputSchema, schema.OutputSchema); err != nil {
		return err
	}
	b.generationMutex.Lock()
	defer b.generationMutex.Unlock()
	b.verbRegistry.mutex.Lock()
	defer b.verbRegistry.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	// Validate schema compatibility
	if existing, exists := b.verbRegistry.schemas[schema.Name]; exists {
		if result := b.validateVerbSchemaCompatibility(existing, schema); !result.Valid {
			return fmt.Errorf("schema compatibility validation failed: %v", result.BreakingChanges)
		}
		schema.CreatedAt = existing.CreatedAt
	}

	// Generate protobuf definition
	if err := b.generateVerbProto(ctx, schema); err != nil {
		return fmt.Errorf("failed to generate verb proto: %w", err)
	}

	// Update registry
	schema.UpdatedAt = time.Now()
	if schema.CreatedAt.IsZero() {
		schema.CreatedAt = schema.UpdatedAt
	}

	b.verbRegistry.schemas[schema.Name] = schema

	return nil
}

// RegisterFactSchema copies metadata and installs a new scalar protobuf definition.
// Existing definitions must be identical. It does not enforce retention or privacy metadata.
func (b *BufIntegration) RegisterFactSchema(ctx context.Context, schema *FactSchema) error {
	if err := b.checkContext(ctx); err != nil {
		return err
	}
	if schema == nil {
		return fmt.Errorf("fact schema is required")
	}
	owned, err := cloneBufFact(schema)
	if err != nil {
		return fmt.Errorf("copy fact schema: %w", err)
	}
	schema = owned
	if err := checkBufSchemaNames(schema.Name, schema.Schema); err != nil {
		return err
	}
	b.generationMutex.Lock()
	defer b.generationMutex.Unlock()
	b.factRegistry.mutex.Lock()
	defer b.factRegistry.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	// Validate schema compatibility
	if existing, exists := b.factRegistry.schemas[schema.Name]; exists {
		if result := b.validateFactSchemaCompatibility(existing, schema); !result.Valid {
			return fmt.Errorf("schema compatibility validation failed: %v", result.BreakingChanges)
		}
		schema.CreatedAt = existing.CreatedAt
	}

	// Generate protobuf definition
	if err := b.generateFactProto(ctx, schema); err != nil {
		return fmt.Errorf("failed to generate fact proto: %w", err)
	}

	// Update registry
	schema.UpdatedAt = time.Now()
	if schema.CreatedAt.IsZero() {
		schema.CreatedAt = schema.UpdatedAt
	}

	b.factRegistry.schemas[schema.Name] = schema

	return nil
}

// GenerateCode runs the workspace's configured Buf plugins. GeneratedFiles lists
// Go protobuf files present in configured output directories, including unchanged files.
// The workspace and plugins must be trusted. One integration owns each workspace.
func (b *BufIntegration) GenerateCode(ctx context.Context) (*CodeGenerationResult, error) {
	return b.generateCode(ctx)
}

// ValidateSchemas runs Buf breaking against local main, then Buf lint.
// Command failures return a non-nil error and an invalid result. Cancellation
// stops the sequence. This is not a full JSON Schema compatibility check.
func (b *BufIntegration) ValidateSchemas(ctx context.Context) (*SchemaValidationResult, error) {
	return b.validateSchemas(ctx)
}

// GetVerbSchema returns an owned snapshot. Nil and zero integrations return false.
func (b *BufIntegration) GetVerbSchema(name string) (*VerbSchema, bool) {
	if b == nil || b.verbRegistry == nil {
		return nil, false
	}
	b.verbRegistry.mutex.RLock()
	defer b.verbRegistry.mutex.RUnlock()
	schema, exists := b.verbRegistry.schemas[name]
	if !exists {
		return nil, false
	}
	copy, err := cloneBufVerb(schema)
	return copy, err == nil
}

// GetFactSchema returns an owned snapshot. Nil and zero integrations return false.
func (b *BufIntegration) GetFactSchema(name string) (*FactSchema, bool) {
	if b == nil || b.factRegistry == nil {
		return nil, false
	}
	b.factRegistry.mutex.RLock()
	defer b.factRegistry.mutex.RUnlock()
	schema, exists := b.factRegistry.schemas[name]
	if !exists {
		return nil, false
	}
	copy, err := cloneBufFact(schema)
	return copy, err == nil
}

// ListVerbSchemas returns owned snapshots. Nil and zero integrations return an empty map.
func (b *BufIntegration) ListVerbSchemas() map[string]*VerbSchema {
	result := make(map[string]*VerbSchema)
	if b == nil || b.verbRegistry == nil {
		return result
	}
	b.verbRegistry.mutex.RLock()
	defer b.verbRegistry.mutex.RUnlock()
	for name, schema := range b.verbRegistry.schemas {
		copy, err := cloneBufVerb(schema)
		if err == nil {
			result[name] = copy
		}
	}
	return result
}

// ListFactSchemas returns owned snapshots. Nil and zero integrations return an empty map.
func (b *BufIntegration) ListFactSchemas() map[string]*FactSchema {
	result := make(map[string]*FactSchema)
	if b == nil || b.factRegistry == nil {
		return result
	}
	b.factRegistry.mutex.RLock()
	defer b.factRegistry.mutex.RUnlock()
	for name, schema := range b.factRegistry.schemas {
		copy, err := cloneBufFact(schema)
		if err == nil {
			result[name] = copy
		}
	}
	return result
}

// generateVerbProto generates protobuf definition for a verb schema
func (b *BufIntegration) generateVerbProto(ctx context.Context, schema *VerbSchema) error {
	protoPath := filepath.Join(b.protoDir, "effectus", "v1", "verbs", fmt.Sprintf("%s.proto", schema.Name))
	return b.installBufProto(ctx, protoPath, b.generateVerbProtoContent(schema))
}

// generateFactProto generates protobuf definition for a fact schema
func (b *BufIntegration) generateFactProto(ctx context.Context, schema *FactSchema) error {
	protoPath := filepath.Join(b.protoDir, "effectus", "v1", "facts", fmt.Sprintf("%s.proto", schema.Name))
	return b.installBufProto(ctx, protoPath, b.generateFactProtoContent(schema))
}

// generateVerbProtoContent generates protobuf content for a verb schema
func (b *BufIntegration) generateVerbProtoContent(schema *VerbSchema) string {
	var builder strings.Builder

	builder.WriteString(`syntax = "proto3";

package effectus.v1.verbs;

import "google/protobuf/any.proto";
import "google/protobuf/timestamp.proto";

option go_package = "github.com/josephjohncox/effectus/gen/effectus/v1/verbs;verbsv1";

`)

	// Generate input message
	builder.WriteString(fmt.Sprintf("// %sInput defines the input for the %s verb\n",
		toCamelCase(schema.Name), schema.Name))
	builder.WriteString(fmt.Sprintf("message %sInput {\n", toCamelCase(schema.Name)))

	fieldNum := 1
	for _, fieldName := range sortedBufFieldNames(schema.InputSchema) {
		fieldType := schema.InputSchema[fieldName]
		builder.WriteString(fmt.Sprintf("  %s %s = %d;\n",
			convertToProtoType(fieldType), fieldName, fieldNum))
		fieldNum++
	}

	builder.WriteString("}\n\n")

	// Generate output message
	builder.WriteString(fmt.Sprintf("// %sOutput defines the output for the %s verb\n",
		toCamelCase(schema.Name), schema.Name))
	builder.WriteString(fmt.Sprintf("message %sOutput {\n", toCamelCase(schema.Name)))

	fieldNum = 1
	for _, fieldName := range sortedBufFieldNames(schema.OutputSchema) {
		fieldType := schema.OutputSchema[fieldName]
		builder.WriteString(fmt.Sprintf("  %s %s = %d;\n",
			convertToProtoType(fieldType), fieldName, fieldNum))
		fieldNum++
	}

	builder.WriteString("}\n\n")

	// Generate service definition
	builder.WriteString(fmt.Sprintf("// %sService provides the %s verb implementation\n",
		toCamelCase(schema.Name), schema.Name))
	builder.WriteString(fmt.Sprintf("service %sService {\n", toCamelCase(schema.Name)))
	builder.WriteString(fmt.Sprintf("  rpc Execute(%sInput) returns (%sOutput);\n",
		toCamelCase(schema.Name), toCamelCase(schema.Name)))
	builder.WriteString("}\n")

	return builder.String()
}

// generateFactProtoContent generates protobuf content for a fact schema
func (b *BufIntegration) generateFactProtoContent(schema *FactSchema) string {
	var builder strings.Builder

	builder.WriteString(`syntax = "proto3";

package effectus.v1.facts;

import "google/protobuf/any.proto";
import "google/protobuf/timestamp.proto";

option go_package = "github.com/josephjohncox/effectus/gen/effectus/v1/facts;factsv1";

`)

	// Generate fact message
	builder.WriteString(fmt.Sprintf("// %s defines the %s fact structure\n",
		toCamelCase(schema.Name), schema.Name))
	builder.WriteString(fmt.Sprintf("message %s {\n", toCamelCase(schema.Name)))

	fieldNum := 1
	for _, fieldName := range sortedBufFieldNames(schema.Schema) {
		fieldType := schema.Schema[fieldName]
		builder.WriteString(fmt.Sprintf("  %s %s = %d;\n",
			convertToProtoType(fieldType), fieldName, fieldNum))
		fieldNum++
	}

	builder.WriteString("}\n")

	return builder.String()
}

// validateVerbSchemaCompatibility validates compatibility between verb schemas
func (b *BufIntegration) validateVerbSchemaCompatibility(existing, new *VerbSchema) *SchemaValidationResult {
	result := &SchemaValidationResult{
		Valid:           true,
		Errors:          []string{},
		Warnings:        []string{},
		BreakingChanges: []string{},
		Suggestions:     []string{},
	}

	// Check for breaking changes in input schema
	for fieldName, fieldType := range existing.InputSchema {
		if newFieldType, exists := new.InputSchema[fieldName]; !exists {
			result.BreakingChanges = append(result.BreakingChanges,
				fmt.Sprintf("removed input field: %s", fieldName))
		} else if !isCompatibleType(fieldType, newFieldType) {
			result.BreakingChanges = append(result.BreakingChanges,
				fmt.Sprintf("incompatible type change for input field %s: %v -> %v",
					fieldName, fieldType, newFieldType))
		}
	}

	// Check for breaking changes in output schema
	for fieldName, fieldType := range existing.OutputSchema {
		if newFieldType, exists := new.OutputSchema[fieldName]; !exists {
			result.BreakingChanges = append(result.BreakingChanges,
				fmt.Sprintf("removed output field: %s", fieldName))
		} else if !isCompatibleType(fieldType, newFieldType) {
			result.BreakingChanges = append(result.BreakingChanges,
				fmt.Sprintf("incompatible type change for output field %s: %v -> %v",
					fieldName, fieldType, newFieldType))
		}
	}

	if len(result.BreakingChanges) > 0 {
		result.Valid = false
		result.Suggestions = append(result.Suggestions,
			"Consider bumping the major version for breaking changes")
	}

	return result
}

// validateFactSchemaCompatibility validates compatibility between fact schemas
func (b *BufIntegration) validateFactSchemaCompatibility(existing, new *FactSchema) *SchemaValidationResult {
	result := &SchemaValidationResult{
		Valid:           true,
		Errors:          []string{},
		Warnings:        []string{},
		BreakingChanges: []string{},
		Suggestions:     []string{},
	}

	// Check for breaking changes in schema
	for fieldName, fieldType := range existing.Schema {
		if newFieldType, exists := new.Schema[fieldName]; !exists {
			result.BreakingChanges = append(result.BreakingChanges,
				fmt.Sprintf("removed field: %s", fieldName))
		} else if !isCompatibleType(fieldType, newFieldType) {
			result.BreakingChanges = append(result.BreakingChanges,
				fmt.Sprintf("incompatible type change for field %s: %v -> %v",
					fieldName, fieldType, newFieldType))
		}
	}

	if len(result.BreakingChanges) > 0 {
		result.Valid = false
		result.Suggestions = append(result.Suggestions,
			"Consider bumping the major version for breaking changes")
	}

	return result
}

// Helper functions

// toCamelCase converts snake_case to CamelCase
func toCamelCase(s string) string {
	words := strings.Split(s, "_")
	for i, word := range words {
		words[i] = strings.Title(word)
	}
	return strings.Join(words, "")
}

// convertToProtoType converts JSON schema type to protobuf type
func convertToProtoType(t interface{}) string {
	switch v := t.(type) {
	case string:
		switch v {
		case "string":
			return "string"
		case "integer":
			return "int64"
		case "number":
			return "double"
		case "boolean":
			return "bool"
		default:
			return "string"
		}
	case map[string]interface{}:
		if typeStr, ok := v["type"].(string); ok {
			return convertToProtoType(typeStr)
		}
		return "google.protobuf.Any"
	default:
		return "google.protobuf.Any"
	}
}

// isCompatibleType checks if two types are compatible
func isCompatibleType(existing, newType interface{}) bool {
	// Simple type compatibility check
	// In a real implementation, this would be more sophisticated
	existingStr := fmt.Sprintf("%v", existing)
	newStr := fmt.Sprintf("%v", newType)
	return existingStr == newStr
}
