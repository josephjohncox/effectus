package schema

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// BufIntegration manages protobuf schemas and code generation using Buf
type BufIntegration struct {
	workspaceRoot string
	protoDir      string
	genDir        string

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

// NewBufIntegration creates a new Buf integration service
func NewBufIntegration(workspaceRoot string) (*BufIntegration, error) {
	protoDir := filepath.Join(workspaceRoot, "proto")
	genDir := filepath.Join(workspaceRoot, "effectus-go", "gen")

	// Ensure directories exist
	if err := os.MkdirAll(protoDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create proto directory: %w", err)
	}

	if err := os.MkdirAll(genDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create gen directory: %w", err)
	}

	integration := &BufIntegration{
		workspaceRoot: workspaceRoot,
		protoDir:      protoDir,
		genDir:        genDir,
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
			// Create default configuration
			b.bufConfig = &BufConfig{
				Version:  "v1",
				Name:     "buf.build/effectus/effectus",
				Deps:     []string{"buf.build/googleapis/googleapis", "buf.build/protocolbuffers/wellknowntypes"},
				Breaking: BufBreakingConfig{Use: []string{"FILE"}},
				Lint:     BufLintConfig{Use: []string{"DEFAULT"}, Except: []string{"UNARY_RPC"}, AllowCommentIgnores: true},
				Build:    BufBuildConfig{Excludes: []string{"examples/", "tests/"}},
			}
			return b.saveBufConfig()
		}
		return fmt.Errorf("failed to read buf config: %w", err)
	}

	if err := yaml.Unmarshal(data, &b.bufConfig); err != nil {
		return fmt.Errorf("failed to parse buf config: %w", err)
	}

	return nil
}

// saveBufConfig saves the buf.yaml configuration
func (b *BufIntegration) saveBufConfig() error {
	configPath := filepath.Join(b.workspaceRoot, "buf.yaml")

	data, err := yaml.Marshal(b.bufConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal buf config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write buf config: %w", err)
	}

	return nil
}

// RegisterVerbSchema registers a new verb interface schema
func (b *BufIntegration) RegisterVerbSchema(ctx context.Context, schema *VerbSchema) error {
	b.verbRegistry.mutex.Lock()
	defer b.verbRegistry.mutex.Unlock()

	// Validate schema compatibility
	if existing, exists := b.verbRegistry.schemas[schema.Name]; exists {
		if result := b.validateVerbSchemaCompatibility(existing, schema); !result.Valid {
			return fmt.Errorf("schema compatibility validation failed: %v", result.Errors)
		}
	}

	// Generate protobuf definition
	if err := b.generateVerbProto(schema); err != nil {
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

// RegisterFactSchema registers a new fact schema
func (b *BufIntegration) RegisterFactSchema(ctx context.Context, schema *FactSchema) error {
	b.factRegistry.mutex.Lock()
	defer b.factRegistry.mutex.Unlock()

	// Validate schema compatibility
	if existing, exists := b.factRegistry.schemas[schema.Name]; exists {
		if result := b.validateFactSchemaCompatibility(existing, schema); !result.Valid {
			return fmt.Errorf("schema compatibility validation failed: %v", result.Errors)
		}
	}

	// Generate protobuf definition
	if err := b.generateFactProto(schema); err != nil {
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

// GenerateCode generates code for all registered schemas
func (b *BufIntegration) GenerateCode(ctx context.Context) (*CodeGenerationResult, error) {
	b.generationMutex.Lock()
	defer b.generationMutex.Unlock()

	startTime := time.Now()
	result := &CodeGenerationResult{
		Success:        true,
		GeneratedFiles: []string{},
		Errors:         []string{},
		Warnings:       []string{},
		Metadata:       make(map[string]interface{}),
	}

	// Run buf generate
	cmd := exec.CommandContext(ctx, "buf", "generate")
	cmd.Dir = b.workspaceRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, fmt.Sprintf("buf generate failed: %v", err))
		result.Errors = append(result.Errors, string(output))
		result.Duration = time.Since(startTime)
		return result, err
	}

	// Parse generated files
	generatedFiles, err := b.findGeneratedFiles()
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to enumerate generated files: %v", err))
	} else {
		result.GeneratedFiles = generatedFiles
	}

	// Update generation timestamp
	b.lastGeneration = time.Now()
	result.Duration = time.Since(startTime)
	result.Metadata["generation_timestamp"] = b.lastGeneration
	result.Metadata["schema_count"] = len(b.verbRegistry.schemas) + len(b.factRegistry.schemas)

	return result, nil
}

// ValidateSchemas validates all registered schemas for compatibility
func (b *BufIntegration) ValidateSchemas(ctx context.Context) (*SchemaValidationResult, error) {
	result := &SchemaValidationResult{
		Valid:           true,
		Errors:          []string{},
		Warnings:        []string{},
		BreakingChanges: []string{},
		Suggestions:     []string{},
	}

	// Run buf breaking
	cmd := exec.CommandContext(ctx, "buf", "breaking", "--against", ".git#branch=main")
	cmd.Dir = b.workspaceRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Breaking changes detected
		result.BreakingChanges = append(result.BreakingChanges, strings.Split(string(output), "\n")...)
		result.Suggestions = append(result.Suggestions, "Consider bumping the major version for breaking changes")
	}

	// Run buf lint
	cmd = exec.CommandContext(ctx, "buf", "lint")
	cmd.Dir = b.workspaceRoot

	output, err = cmd.CombinedOutput()
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("buf lint failed: %v", err))
		result.Errors = append(result.Errors, strings.Split(string(output), "\n")...)
	}

	return result, nil
}

// GetVerbSchema retrieves a verb schema by name
func (b *BufIntegration) GetVerbSchema(name string) (*VerbSchema, bool) {
	b.verbRegistry.mutex.RLock()
	defer b.verbRegistry.mutex.RUnlock()

	schema, exists := b.verbRegistry.schemas[name]
	return schema, exists
}

// GetFactSchema retrieves a fact schema by name
func (b *BufIntegration) GetFactSchema(name string) (*FactSchema, bool) {
	b.factRegistry.mutex.RLock()
	defer b.factRegistry.mutex.RUnlock()

	schema, exists := b.factRegistry.schemas[name]
	return schema, exists
}

// ListVerbSchemas returns all registered verb schemas
func (b *BufIntegration) ListVerbSchemas() map[string]*VerbSchema {
	b.verbRegistry.mutex.RLock()
	defer b.verbRegistry.mutex.RUnlock()

	result := make(map[string]*VerbSchema)
	for name, schema := range b.verbRegistry.schemas {
		result[name] = schema
	}
	return result
}

// ListFactSchemas returns all registered fact schemas
func (b *BufIntegration) ListFactSchemas() map[string]*FactSchema {
	b.factRegistry.mutex.RLock()
	defer b.factRegistry.mutex.RUnlock()

	result := make(map[string]*FactSchema)
	for name, schema := range b.factRegistry.schemas {
		result[name] = schema
	}
	return result
}

// generateVerbProto generates protobuf definition for a verb schema
func (b *BufIntegration) generateVerbProto(schema *VerbSchema) error {
	protoPath := filepath.Join(b.protoDir, "effectus", "v1", "verbs", fmt.Sprintf("%s.proto", schema.Name))

	if err := os.MkdirAll(filepath.Dir(protoPath), 0755); err != nil {
		return fmt.Errorf("failed to create verb proto directory: %w", err)
	}

	protoContent := b.generateVerbProtoContent(schema)

	if err := os.WriteFile(protoPath, []byte(protoContent), 0644); err != nil {
		return fmt.Errorf("failed to write verb proto file: %w", err)
	}

	return nil
}

// generateFactProto generates protobuf definition for a fact schema
func (b *BufIntegration) generateFactProto(schema *FactSchema) error {
	protoPath := filepath.Join(b.protoDir, "effectus", "v1", "facts", fmt.Sprintf("%s.proto", schema.Name))

	if err := os.MkdirAll(filepath.Dir(protoPath), 0755); err != nil {
		return fmt.Errorf("failed to create fact proto directory: %w", err)
	}

	protoContent := b.generateFactProtoContent(schema)

	if err := os.WriteFile(protoPath, []byte(protoContent), 0644); err != nil {
		return fmt.Errorf("failed to write fact proto file: %w", err)
	}

	return nil
}

// generateVerbProtoContent generates protobuf content for a verb schema
func (b *BufIntegration) generateVerbProtoContent(schema *VerbSchema) string {
	var builder strings.Builder

	builder.WriteString(`syntax = "proto3";

package effectus.v1.verbs;

import "google/protobuf/any.proto";
import "google/protobuf/timestamp.proto";

option go_package = "github.com/effectus/effectus-go/gen/effectus/v1/verbs;verbsv1";

`)

	// Generate input message
	builder.WriteString(fmt.Sprintf("// %sInput defines the input for the %s verb\n",
		toCamelCase(schema.Name), schema.Name))
	builder.WriteString(fmt.Sprintf("message %sInput {\n", toCamelCase(schema.Name)))

	fieldNum := 1
	for fieldName, fieldType := range schema.InputSchema {
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
	for fieldName, fieldType := range schema.OutputSchema {
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

option go_package = "github.com/effectus/effectus-go/gen/effectus/v1/facts;factsv1";

`)

	// Generate fact message
	builder.WriteString(fmt.Sprintf("// %s defines the %s fact structure\n",
		toCamelCase(schema.Name), schema.Name))
	builder.WriteString(fmt.Sprintf("message %s {\n", toCamelCase(schema.Name)))

	fieldNum := 1
	for fieldName, fieldType := range schema.Schema {
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

// findGeneratedFiles finds all files generated by buf generate
func (b *BufIntegration) findGeneratedFiles() ([]string, error) {
	var files []string

	err := filepath.Walk(b.genDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && (strings.HasSuffix(path, ".pb.go") || strings.HasSuffix(path, "_grpc.pb.go")) {
			relPath, err := filepath.Rel(b.workspaceRoot, path)
			if err != nil {
				return err
			}
			files = append(files, relPath)
		}

		return nil
	})

	return files, err
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
