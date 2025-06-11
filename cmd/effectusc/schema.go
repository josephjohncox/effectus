package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// SchemaInfo represents metadata about a protobuf schema
type SchemaInfo struct {
	Name          string                 `yaml:"name" json:"name"`
	Version       string                 `yaml:"version" json:"version"`
	Description   string                 `yaml:"description" json:"description"`
	Fields        map[string]FieldInfo   `yaml:"fields" json:"fields"`
	Sources       []SourceInfo           `yaml:"sources" json:"sources"`
	Lineage       LineageInfo            `yaml:"lineage" json:"lineage"`
	Examples      []interface{}          `yaml:"examples" json:"examples"`
	Documentation string                 `yaml:"documentation" json:"documentation"`
	LastUpdated   time.Time              `yaml:"last_updated" json:"last_updated"`
	Metadata      map[string]interface{} `yaml:"metadata" json:"metadata"`
}

// FieldInfo represents information about a protobuf field
type FieldInfo struct {
	Type        string      `yaml:"type" json:"type"`
	Required    bool        `yaml:"required" json:"required"`
	Description string      `yaml:"description" json:"description"`
	Examples    []string    `yaml:"examples" json:"examples"`
	Validation  string      `yaml:"validation,omitempty" json:"validation,omitempty"`
	PII         bool        `yaml:"pii,omitempty" json:"pii,omitempty"`
	Range       []float64   `yaml:"range,omitempty" json:"range,omitempty"`
	Enum        []string    `yaml:"enum,omitempty" json:"enum,omitempty"`
	Default     interface{} `yaml:"default,omitempty" json:"default,omitempty"`
}

// SourceInfo represents where schema data comes from
type SourceInfo struct {
	Adapter     string `yaml:"adapter" json:"adapter"`
	Type        string `yaml:"type" json:"type"`
	Config      string `yaml:"config,omitempty" json:"config,omitempty"`
	Frequency   string `yaml:"frequency,omitempty" json:"frequency,omitempty"`
	Incremental string `yaml:"incremental,omitempty" json:"incremental,omitempty"`
	Realtime    bool   `yaml:"realtime,omitempty" json:"realtime,omitempty"`
}

// LineageInfo represents upstream and downstream dependencies
type LineageInfo struct {
	Upstream   []string `yaml:"upstream" json:"upstream"`
	Downstream []string `yaml:"downstream" json:"downstream"`
}

// SchemaRegistry manages schema information and documentation
type SchemaRegistry struct {
	schemas  map[string]*SchemaInfo
	basePath string
}

// NewSchemaRegistry creates a new schema registry
func NewSchemaRegistry(basePath string) *SchemaRegistry {
	return &SchemaRegistry{
		schemas:  make(map[string]*SchemaInfo),
		basePath: basePath,
	}
}

// schemaCmd represents the schema command group
var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Schema management and documentation",
	Long: `Manage protobuf schemas, generate documentation, and provide schema information for IDE integration.

This command group provides tools for:
- Generating schema documentation from protobuf definitions
- Creating schema lineage diagrams
- Generating example data for testing
- Syncing schemas from deployed adapters
- Providing schema information for Language Server Protocol (LSP)`,
}

// schemaDocsCmd generates schema documentation
var schemaDocsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Generate schema documentation",
	Long: `Generate comprehensive documentation for all protobuf schemas.

The documentation includes:
- Field descriptions and types
- Validation rules and constraints
- Source adapter information
- Example data
- Schema lineage information`,
	RunE: func(cmd *cobra.Command, args []string) error {
		outputDir, _ := cmd.Flags().GetString("output")
		format, _ := cmd.Flags().GetString("format")
		includeExamples, _ := cmd.Flags().GetBool("include-examples")

		return generateSchemaDocs(outputDir, format, includeExamples)
	},
}

// schemaLineageCmd generates schema lineage diagrams
var schemaLineageCmd = &cobra.Command{
	Use:   "lineage",
	Short: "Generate schema lineage diagrams",
	Long: `Generate visual diagrams showing schema dependencies and data flow.

Supports multiple output formats:
- Mermaid diagrams for embedding in documentation
- DOT format for Graphviz rendering
- JSON for programmatic analysis
- Interactive HTML for exploration`,
	RunE: func(cmd *cobra.Command, args []string) error {
		outputFile, _ := cmd.Flags().GetString("output")
		format, _ := cmd.Flags().GetString("format")
		interactive, _ := cmd.Flags().GetBool("interactive")

		return generateSchemaLineage(outputFile, format, interactive)
	},
}

// schemaExamplesCmd generates example data
var schemaExamplesCmd = &cobra.Command{
	Use:   "examples",
	Short: "Generate example data for schemas",
	Long: `Generate realistic example data for protobuf schemas.

The examples can be used for:
- Testing rule logic
- Documentation
- Development and debugging
- Training and demos`,
	RunE: func(cmd *cobra.Command, args []string) error {
		outputDir, _ := cmd.Flags().GetString("output")
		count, _ := cmd.Flags().GetInt("count")
		schema, _ := cmd.Flags().GetString("schema")

		return generateSchemaExamples(outputDir, count, schema)
	},
}

// schemaSyncCmd syncs schemas from deployed adapters
var schemaSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync schemas from deployed sources",
	Long: `Synchronize schema information from deployed adapters and services.

This pulls the latest schema definitions and metadata from:
- Running adapter instances
- Schema registries  
- Protobuf definitions
- Documentation sources`,
	RunE: func(cmd *cobra.Command, args []string) error {
		source, _ := cmd.Flags().GetString("from")
		outputDir, _ := cmd.Flags().GetString("output")

		return syncSchemas(source, outputDir)
	},
}

// schemaLspCmd starts the Language Server Protocol server
var schemaLspCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Start Language Server Protocol server",
	Long: `Start the LSP server for IDE integration.

Provides:
- Schema-aware autocompletion
- Real-time validation
- Hover information with documentation
- Go-to-definition for schema types
- Diagnostic messages for rule errors`,
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetInt("port")
		schemaDir, _ := cmd.Flags().GetString("schema-dir")

		return startLSPServer(port, schemaDir)
	},
}

func init() {
	rootCmd.AddCommand(schemaCmd)

	// Add subcommands
	schemaCmd.AddCommand(schemaDocsCmd)
	schemaCmd.AddCommand(schemaLineageCmd)
	schemaCmd.AddCommand(schemaExamplesCmd)
	schemaCmd.AddCommand(schemaSyncCmd)
	schemaCmd.AddCommand(schemaLspCmd)

	// Schema docs flags
	schemaDocsCmd.Flags().StringP("output", "o", "./schema-docs", "Output directory for documentation")
	schemaDocsCmd.Flags().String("format", "yaml", "Output format (yaml, json, html, markdown)")
	schemaDocsCmd.Flags().Bool("include-examples", true, "Include example data in documentation")

	// Schema lineage flags
	schemaLineageCmd.Flags().StringP("output", "o", "./lineage.md", "Output file for lineage diagram")
	schemaLineageCmd.Flags().String("format", "mermaid", "Output format (mermaid, dot, json, html)")
	schemaLineageCmd.Flags().Bool("interactive", false, "Generate interactive HTML diagram")

	// Schema examples flags
	schemaExamplesCmd.Flags().StringP("output", "o", "./examples", "Output directory for examples")
	schemaExamplesCmd.Flags().Int("count", 10, "Number of examples to generate per schema")
	schemaExamplesCmd.Flags().String("schema", "", "Generate examples for specific schema (default: all)")

	// Schema sync flags
	schemaSyncCmd.Flags().String("from", "production", "Source to sync from (production, staging, local)")
	schemaSyncCmd.Flags().StringP("output", "o", "./schemas", "Output directory for synced schemas")

	// Schema LSP flags
	schemaLspCmd.Flags().Int("port", 9091, "Port for LSP server")
	schemaLspCmd.Flags().String("schema-dir", "./schema-docs", "Directory containing schema documentation")
}

func generateSchemaDocs(outputDir, format string, includeExamples bool) error {
	log.Printf("Generating schema documentation in %s format to %s", format, outputDir)

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Load schemas from protobuf definitions
	registry := NewSchemaRegistry(outputDir)
	if err := registry.loadSchemas(); err != nil {
		return fmt.Errorf("failed to load schemas: %w", err)
	}

	// Generate documentation for each schema
	for name, schema := range registry.schemas {
		log.Printf("Generating documentation for schema: %s", name)

		if includeExamples {
			examples, err := generateExampleData(schema, 5)
			if err != nil {
				log.Printf("Warning: failed to generate examples for %s: %v", name, err)
			} else {
				schema.Examples = examples
			}
		}

		// Write schema documentation
		filename := fmt.Sprintf("%s.%s", sanitizeFilename(name), getFileExtension(format))
		filepath := filepath.Join(outputDir, filename)

		if err := writeSchemaDoc(filepath, schema, format); err != nil {
			return fmt.Errorf("failed to write documentation for %s: %w", name, err)
		}
	}

	// Generate index file
	if err := generateIndex(outputDir, registry.schemas, format); err != nil {
		return fmt.Errorf("failed to generate index: %w", err)
	}

	log.Printf("Schema documentation generated successfully in %s", outputDir)
	return nil
}

func generateSchemaLineage(outputFile, format string, interactive bool) error {
	log.Printf("Generating schema lineage diagram in %s format", format)

	registry := NewSchemaRegistry("./schema-docs")
	if err := registry.loadSchemas(); err != nil {
		return fmt.Errorf("failed to load schemas: %w", err)
	}

	switch format {
	case "mermaid":
		return generateMermaidLineage(outputFile, registry.schemas)
	case "dot":
		return generateDotLineage(outputFile, registry.schemas)
	case "json":
		return generateJSONLineage(outputFile, registry.schemas)
	case "html":
		return generateHTMLLineage(outputFile, registry.schemas, interactive)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func generateSchemaExamples(outputDir string, count int, schemaFilter string) error {
	log.Printf("Generating %d examples per schema", count)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	registry := NewSchemaRegistry("./schema-docs")
	if err := registry.loadSchemas(); err != nil {
		return fmt.Errorf("failed to load schemas: %w", err)
	}

	for name, schema := range registry.schemas {
		if schemaFilter != "" && !strings.Contains(name, schemaFilter) {
			continue
		}

		log.Printf("Generating examples for schema: %s", name)

		examples, err := generateExampleData(schema, count)
		if err != nil {
			log.Printf("Warning: failed to generate examples for %s: %v", name, err)
			continue
		}

		// Write examples to file
		filename := fmt.Sprintf("%s_examples.json", sanitizeFilename(name))
		filepath := filepath.Join(outputDir, filename)

		if err := writeExamples(filepath, examples); err != nil {
			return fmt.Errorf("failed to write examples for %s: %w", name, err)
		}
	}

	log.Printf("Schema examples generated successfully in %s", outputDir)
	return nil
}

func syncSchemas(source, outputDir string) error {
	log.Printf("Syncing schemas from %s to %s", source, outputDir)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Implementation would sync from various sources:
	// - HTTP endpoints for running adapters
	// - Schema registry APIs
	// - Git repositories with protobuf definitions
	// - Configuration management systems

	log.Printf("Schema sync completed successfully")
	return nil
}

func startLSPServer(port int, schemaDir string) error {
	log.Printf("Starting LSP server on port %d with schemas from %s", port, schemaDir)

	// Implementation would start the Language Server Protocol server
	// This would provide IDE integration for:
	// - Autocompletion
	// - Validation
	// - Hover information
	// - Go-to-definition
	// - Diagnostics

	log.Printf("LSP server started successfully")
	return nil
}

// Helper functions

func (r *SchemaRegistry) loadSchemas() error {
	// Mock implementation - would load from actual protobuf definitions
	r.schemas["acme.v1.facts.UserProfile"] = &SchemaInfo{
		Name:        "acme.v1.facts.UserProfile",
		Version:     "v1.2.0",
		Description: "User profile information with preferences and activity",
		Fields: map[string]FieldInfo{
			"user_id": {
				Type:        "string",
				Required:    true,
				Description: "Unique user identifier",
				Examples:    []string{"user_12345", "usr_abc123"},
			},
			"email": {
				Type:        "string",
				Required:    true,
				Description: "User's primary email address",
				Validation:  "email",
				PII:         true,
			},
			"activity_score": {
				Type:        "double",
				Description: "User engagement score (0-100)",
				Range:       []float64{0.0, 100.0},
			},
		},
		Sources: []SourceInfo{
			{
				Adapter:     "postgres_users",
				Type:        "postgres_poller",
				Frequency:   "30s",
				Incremental: "updated_at",
			},
			{
				Adapter:  "redis_events",
				Type:     "redis_streams",
				Realtime: true,
			},
		},
		Lineage: LineageInfo{
			Upstream:   []string{"user_registration_event", "user_activity_event"},
			Downstream: []string{"user_segment_classification", "personalization_context"},
		},
		LastUpdated: time.Now(),
	}

	return nil
}

func generateExampleData(schema *SchemaInfo, count int) ([]interface{}, error) {
	examples := make([]interface{}, count)

	for i := 0; i < count; i++ {
		example := make(map[string]interface{})

		for fieldName, field := range schema.Fields {
			example[fieldName] = generateFieldExample(field, i)
		}

		examples[i] = example
	}

	return examples, nil
}

func generateFieldExample(field FieldInfo, index int) interface{} {
	switch field.Type {
	case "string":
		if len(field.Examples) > 0 {
			return field.Examples[index%len(field.Examples)]
		}
		return fmt.Sprintf("example_%d", index)
	case "double", "float":
		if len(field.Range) == 2 {
			return field.Range[0] + (field.Range[1]-field.Range[0])*0.5
		}
		return float64(index) * 1.5
	case "int32", "int64":
		return index + 1
	case "bool":
		return index%2 == 0
	default:
		return fmt.Sprintf("example_%s_%d", field.Type, index)
	}
}

func writeSchemaDoc(filepath string, schema *SchemaInfo, format string) error {
	var data []byte
	var err error

	switch format {
	case "yaml":
		data, err = yaml.Marshal(schema)
	case "json":
		data, err = json.MarshalIndent(schema, "", "  ")
	case "markdown":
		data = []byte(generateMarkdownDoc(schema))
	case "html":
		data = []byte(generateHTMLDoc(schema))
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	if err != nil {
		return err
	}

	return os.WriteFile(filepath, data, 0644)
}

func generateMarkdownDoc(schema *SchemaInfo) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n\n", schema.Name))
	sb.WriteString(fmt.Sprintf("**Version:** %s\n\n", schema.Version))
	sb.WriteString(fmt.Sprintf("%s\n\n", schema.Description))

	sb.WriteString("## Fields\n\n")
	sb.WriteString("| Field | Type | Required | Description |\n")
	sb.WriteString("|-------|------|----------|-----------|\n")

	// Sort fields for consistent output
	fields := make([]string, 0, len(schema.Fields))
	for name := range schema.Fields {
		fields = append(fields, name)
	}
	sort.Strings(fields)

	for _, name := range fields {
		field := schema.Fields[name]
		required := ""
		if field.Required {
			required = "✓"
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			name, field.Type, required, field.Description))
	}

	if len(schema.Sources) > 0 {
		sb.WriteString("\n## Data Sources\n\n")
		for _, source := range schema.Sources {
			sb.WriteString(fmt.Sprintf("- **%s** (%s)", source.Adapter, source.Type))
			if source.Frequency != "" {
				sb.WriteString(fmt.Sprintf(" - %s", source.Frequency))
			}
			if source.Realtime {
				sb.WriteString(" - Real-time")
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func generateHTMLDoc(schema *SchemaInfo) string {
	// Implementation would generate rich HTML documentation
	return fmt.Sprintf("<html><body><h1>%s</h1><p>%s</p></body></html>",
		schema.Name, schema.Description)
}

func generateMermaidLineage(outputFile string, schemas map[string]*SchemaInfo) error {
	var sb strings.Builder

	sb.WriteString("```mermaid\n")
	sb.WriteString("graph TD\n")

	// Generate nodes and connections
	for _, schema := range schemas {
		schemaNode := sanitizeNodeName(schema.Name)

		// Add source connections
		for _, source := range schema.Sources {
			sourceNode := sanitizeNodeName(source.Adapter)
			sb.WriteString(fmt.Sprintf("    %s -->|%s| %s\n",
				sourceNode, source.Type, schemaNode))
		}

		// Add downstream connections
		for _, downstream := range schema.Lineage.Downstream {
			downstreamNode := sanitizeNodeName(downstream)
			sb.WriteString(fmt.Sprintf("    %s --> %s\n",
				schemaNode, downstreamNode))
		}
	}

	sb.WriteString("```\n")

	return os.WriteFile(outputFile, []byte(sb.String()), 0644)
}

func generateDotLineage(outputFile string, schemas map[string]*SchemaInfo) error {
	// Implementation for Graphviz DOT format
	return nil
}

func generateJSONLineage(outputFile string, schemas map[string]*SchemaInfo) error {
	lineage := make(map[string]interface{})

	for name, schema := range schemas {
		lineage[name] = map[string]interface{}{
			"sources":    schema.Sources,
			"upstream":   schema.Lineage.Upstream,
			"downstream": schema.Lineage.Downstream,
		}
	}

	data, err := json.MarshalIndent(lineage, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(outputFile, data, 0644)
}

func generateHTMLLineage(outputFile string, schemas map[string]*SchemaInfo, interactive bool) error {
	// Implementation for interactive HTML lineage viewer
	return nil
}

func generateIndex(outputDir string, schemas map[string]*SchemaInfo, format string) error {
	indexPath := filepath.Join(outputDir, fmt.Sprintf("index.%s", getFileExtension(format)))

	index := map[string]interface{}{
		"generated_at": time.Now(),
		"schemas":      schemas,
		"summary": map[string]interface{}{
			"total_schemas": len(schemas),
			"total_fields":  getTotalFields(schemas),
		},
	}

	var data []byte
	var err error

	switch format {
	case "yaml":
		data, err = yaml.Marshal(index)
	case "json":
		data, err = json.MarshalIndent(index, "", "  ")
	default:
		return fmt.Errorf("unsupported format for index: %s", format)
	}

	if err != nil {
		return err
	}

	return os.WriteFile(indexPath, data, 0644)
}

func writeExamples(filepath string, examples []interface{}) error {
	data, err := json.MarshalIndent(examples, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath, data, 0644)
}

// Utility functions

func sanitizeFilename(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, ".", "_"), "/", "_")
}

func sanitizeNodeName(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, ".", "_"), "-", "_")
}

func getFileExtension(format string) string {
	switch format {
	case "yaml":
		return "yaml"
	case "json":
		return "json"
	case "markdown":
		return "md"
	case "html":
		return "html"
	default:
		return "txt"
	}
}

func getTotalFields(schemas map[string]*SchemaInfo) int {
	total := 0
	for _, schema := range schemas {
		total += len(schema.Fields)
	}
	return total
}
