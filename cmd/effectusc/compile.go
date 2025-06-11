package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/effectus/effectus-go/compiler"
)

// compileCmd represents the compile command
var compileCmd = &cobra.Command{
	Use:   "compile",
	Short: "Compile Effectus rules to optimized bytecode",
	Long: `Compile business-friendly Effectus rules (.eff/.effx) to optimized bytecode.

The compilation process:
1. Parse business-friendly rule syntax
2. Transpile to valid expr expressions using schema information
3. Validate transpiled code against schemas
4. Optimize for performance
5. Generate bytecode for runtime execution

Examples:
  # Compile all rules in a directory
  effectusc compile ./rules --output ./dist

  # Compile specific rule file
  effectusc compile ./rules/welcome_user.eff --output ./dist/welcome_user.effb

  # Compile with optimization and validation
  effectusc compile ./rules --optimize --strict --schema-dir ./schema-docs`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("source path is required")
		}

		sourcePath := args[0]
		outputDir, _ := cmd.Flags().GetString("output")
		optimize, _ := cmd.Flags().GetBool("optimize")
		strict, _ := cmd.Flags().GetBool("strict")
		schemaDir, _ := cmd.Flags().GetString("schema-dir")
		format, _ := cmd.Flags().GetString("format")

		return compileRules(sourcePath, outputDir, schemaDir, optimize, strict, format)
	},
}

// testCompileCmd tests rule compilation without output
var testCompileCmd = &cobra.Command{
	Use:   "test-compile",
	Short: "Test compilation without generating output",
	Long: `Test rule compilation to validate syntax and schema compatibility.

This command performs all compilation steps but doesn't generate output files.
Useful for CI/CD validation and development feedback.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("source path is required")
		}

		sourcePath := args[0]
		schemaDir, _ := cmd.Flags().GetString("schema-dir")
		strict, _ := cmd.Flags().GetBool("strict")

		return testCompileRules(sourcePath, schemaDir, strict)
	},
}

// transpileCmd shows transpiled expr code for debugging
var transpileCmd = &cobra.Command{
	Use:   "transpile",
	Short: "Show transpiled expr code for debugging",
	Long: `Transpile business-friendly rule syntax to expr and display the result.

This is useful for:
- Debugging transpilation issues
- Understanding generated expr code
- Validating business rule logic
- Learning expr syntax`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("rule file is required")
		}

		ruleFile := args[0]
		schemaDir, _ := cmd.Flags().GetString("schema-dir")

		return transpileAndShow(ruleFile, schemaDir)
	},
}

func init() {
	rootCmd.AddCommand(compileCmd)
	rootCmd.AddCommand(testCompileCmd)
	rootCmd.AddCommand(transpileCmd)

	// Compile command flags
	compileCmd.Flags().StringP("output", "o", "./dist", "Output directory for compiled rules")
	compileCmd.Flags().Bool("optimize", false, "Enable optimization passes")
	compileCmd.Flags().Bool("strict", false, "Enable strict validation mode")
	compileCmd.Flags().String("schema-dir", "./schema-docs", "Directory containing schema documentation")
	compileCmd.Flags().String("format", "bytecode", "Output format (bytecode, expr, json)")

	// Test compile command flags
	testCompileCmd.Flags().String("schema-dir", "./schema-docs", "Directory containing schema documentation")
	testCompileCmd.Flags().Bool("strict", false, "Enable strict validation mode")

	// Transpile command flags
	transpileCmd.Flags().String("schema-dir", "./schema-docs", "Directory containing schema documentation")
}

func compileRules(sourcePath, outputDir, schemaDir string, optimize, strict bool, format string) error {
	log.Printf("Compiling rules from %s to %s", sourcePath, outputDir)

	// Load schemas
	schemas, err := loadSchemas(schemaDir)
	if err != nil {
		return fmt.Errorf("failed to load schemas: %w", err)
	}

	// Create transpiler
	transpiler := compiler.NewTranspiler(schemas)

	// Find rule files
	ruleFiles, err := findRuleFiles(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to find rule files: %w", err)
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Compilation statistics
	stats := &CompilationStats{
		TotalRules: len(ruleFiles),
	}

	// Compile each rule file
	for _, ruleFile := range ruleFiles {
		log.Printf("Compiling: %s", ruleFile)

		result, err := compileRuleFile(transpiler, ruleFile, optimize, strict)
		if err != nil {
			stats.Failed++
			log.Printf("Failed to compile %s: %v", ruleFile, err)
			if strict {
				return fmt.Errorf("compilation failed in strict mode: %w", err)
			}
			continue
		}

		// Generate output file
		outputFile := getOutputFilename(ruleFile, outputDir, format)
		if err := writeCompiledRule(outputFile, result, format); err != nil {
			stats.Failed++
			log.Printf("Failed to write output for %s: %v", ruleFile, err)
			continue
		}

		stats.Successful++
		log.Printf("✓ Compiled: %s -> %s", ruleFile, outputFile)
	}

	// Print compilation summary
	printCompilationSummary(stats)

	if stats.Failed > 0 && strict {
		return fmt.Errorf("compilation failed: %d errors", stats.Failed)
	}

	return nil
}

func testCompileRules(sourcePath, schemaDir string, strict bool) error {
	log.Printf("Testing compilation of rules in %s", sourcePath)

	// Load schemas
	schemas, err := loadSchemas(schemaDir)
	if err != nil {
		return fmt.Errorf("failed to load schemas: %w", err)
	}

	// Create transpiler
	transpiler := compiler.NewTranspiler(schemas)

	// Find rule files
	ruleFiles, err := findRuleFiles(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to find rule files: %w", err)
	}

	// Test compilation for each file
	stats := &CompilationStats{
		TotalRules: len(ruleFiles),
	}

	for _, ruleFile := range ruleFiles {
		log.Printf("Testing: %s", ruleFile)

		_, err := compileRuleFile(transpiler, ruleFile, false, strict)
		if err != nil {
			stats.Failed++
			log.Printf("✗ %s: %v", ruleFile, err)
			continue
		}

		stats.Successful++
		log.Printf("✓ %s: OK", ruleFile)
	}

	// Print test summary
	printCompilationSummary(stats)

	if stats.Failed > 0 {
		return fmt.Errorf("compilation test failed: %d errors", stats.Failed)
	}

	log.Printf("All rules compiled successfully!")
	return nil
}

func transpileAndShow(ruleFile, schemaDir string) error {
	log.Printf("Transpiling rule: %s", ruleFile)

	// Load schemas
	schemas, err := loadSchemas(schemaDir)
	if err != nil {
		return fmt.Errorf("failed to load schemas: %w", err)
	}

	// Create transpiler
	transpiler := compiler.NewTranspiler(schemas)

	// Read rule file
	ruleContent, err := ioutil.ReadFile(ruleFile)
	if err != nil {
		return fmt.Errorf("failed to read rule file: %w", err)
	}

	// Transpile rule
	result, err := transpiler.TranspileAndValidate(string(ruleContent))
	if err != nil {
		return fmt.Errorf("transpilation failed: %w", err)
	}

	// Display results
	fmt.Printf("📄 Original Rule (%s):\n", ruleFile)
	fmt.Printf("─────────────────────────────────────────────\n")
	fmt.Printf("%s\n\n", result.OriginalRule)

	fmt.Printf("🔄 Transpiled Expr Code:\n")
	fmt.Printf("─────────────────────────────────────────────\n")
	fmt.Printf("%s\n\n", result.ExprCode)

	fmt.Printf("✅ Transpilation successful!\n")
	fmt.Printf("   - Syntax validation: ✓\n")
	fmt.Printf("   - Schema validation: ✓\n")
	fmt.Printf("   - Expr compilation: ✓\n")

	return nil
}

// CompilationStats tracks compilation results
type CompilationStats struct {
	TotalRules int
	Successful int
	Failed     int
}

// CompiledRule represents a compiled rule with metadata
type CompiledRule struct {
	Name         string                 `json:"name"`
	OriginalRule string                 `json:"original_rule"`
	ExprCode     string                 `json:"expr_code"`
	ByteCode     []byte                 `json:"byte_code,omitempty"`
	Dependencies []string               `json:"dependencies"`
	Performance  *PerformanceHints      `json:"performance,omitempty"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// PerformanceHints provides optimization suggestions
type PerformanceHints struct {
	IndexSuggestions    []string `json:"index_suggestions"`
	OptimizationLevel   string   `json:"optimization_level"`
	EstimatedThroughput int      `json:"estimated_throughput"`
}

func compileRuleFile(transpiler *compiler.Transpiler, ruleFile string, optimize, strict bool) (*CompiledRule, error) {
	// Read rule file
	ruleContent, err := ioutil.ReadFile(ruleFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read rule file: %w", err)
	}

	// Transpile rule
	result, err := transpiler.TranspileAndValidate(string(ruleContent))
	if err != nil {
		return nil, fmt.Errorf("transpilation failed: %w", err)
	}

	// Extract rule name from file
	ruleName := strings.TrimSuffix(filepath.Base(ruleFile), filepath.Ext(ruleFile))

	compiled := &CompiledRule{
		Name:         ruleName,
		OriginalRule: result.OriginalRule,
		ExprCode:     result.ExprCode,
		Dependencies: extractDependencies(result.ExprCode),
		Metadata: map[string]interface{}{
			"source_file":      ruleFile,
			"compiled_at":      getCurrentTimestamp(),
			"compiler_version": "1.0.0",
		},
	}

	// Apply optimizations if requested
	if optimize {
		if err := applyOptimizations(compiled); err != nil {
			return nil, fmt.Errorf("optimization failed: %w", err)
		}
	}

	return compiled, nil
}

func findRuleFiles(sourcePath string) ([]string, error) {
	var ruleFiles []string

	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && (strings.HasSuffix(path, ".eff") || strings.HasSuffix(path, ".effx")) {
			ruleFiles = append(ruleFiles, path)
		}

		return nil
	})

	return ruleFiles, err
}

func loadSchemas(schemaDir string) (map[string]*compiler.SchemaInfo, error) {
	schemas := make(map[string]*compiler.SchemaInfo)

	err := filepath.Walk(schemaDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && (strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
			schema, err := loadSchemaFile(path)
			if err != nil {
				log.Printf("Warning: failed to load schema %s: %v", path, err)
				return nil // Continue with other files
			}

			schemas[schema.Name] = schema
		}

		return nil
	})

	return schemas, err
}

func loadSchemaFile(path string) (*compiler.SchemaInfo, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc struct {
		Schema struct {
			Name   string `yaml:"name"`
			Fields map[string]struct {
				Type string `yaml:"type"`
			} `yaml:"fields"`
		} `yaml:"schema"`
	}

	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}

	schema := &compiler.SchemaInfo{
		Name:   doc.Schema.Name,
		Fields: make(map[string]compiler.FieldInfo),
	}

	for fieldName, field := range doc.Schema.Fields {
		schema.Fields[fieldName] = compiler.FieldInfo{
			Type:        field.Type,
			IsTimestamp: field.Type == "google.protobuf.Timestamp" || field.Type == "timestamp",
		}
	}

	return schema, nil
}

func getOutputFilename(ruleFile, outputDir, format string) string {
	baseName := strings.TrimSuffix(filepath.Base(ruleFile), filepath.Ext(ruleFile))

	var ext string
	switch format {
	case "bytecode":
		ext = ".effb"
	case "expr":
		ext = ".expr"
	case "json":
		ext = ".json"
	default:
		ext = ".effb"
	}

	return filepath.Join(outputDir, baseName+ext)
}

func writeCompiledRule(outputFile string, rule *CompiledRule, format string) error {
	var data []byte
	var err error

	switch format {
	case "json":
		data, err = json.MarshalIndent(rule, "", "  ")
	case "expr":
		data = []byte(rule.ExprCode)
	case "bytecode":
		// In a real implementation, this would be optimized bytecode
		data, err = json.Marshal(rule)
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}

	if err != nil {
		return err
	}

	return ioutil.WriteFile(outputFile, data, 0644)
}

func extractDependencies(exprCode string) []string {
	var deps []string

	// Extract schema dependencies from expr code
	// This is a simplified implementation
	if strings.Contains(exprCode, "UserProfile") {
		deps = append(deps, "acme.v1.facts.UserProfile")
	}
	if strings.Contains(exprCode, "OrderEvent") {
		deps = append(deps, "acme.v1.facts.OrderEvent")
	}

	return deps
}

func applyOptimizations(rule *CompiledRule) error {
	// Apply performance optimizations
	rule.Performance = &PerformanceHints{
		OptimizationLevel:   "O2",
		EstimatedThroughput: 10000, // facts/second
		IndexSuggestions: []string{
			"Consider indexing activity_score for range queries",
			"Consider indexing created_at for temporal queries",
		},
	}

	return nil
}

func printCompilationSummary(stats *CompilationStats) {
	fmt.Printf("\n📊 Compilation Summary:\n")
	fmt.Printf("─────────────────────────\n")
	fmt.Printf("Total rules:      %d\n", stats.TotalRules)
	fmt.Printf("Successful:       %d ✓\n", stats.Successful)
	fmt.Printf("Failed:          %d ✗\n", stats.Failed)

	if stats.TotalRules > 0 {
		successRate := float64(stats.Successful) / float64(stats.TotalRules) * 100
		fmt.Printf("Success rate:    %.1f%%\n", successRate)
	}

	fmt.Printf("\n")
}

func getCurrentTimestamp() string {
	return time.Now().Format(time.RFC3339)
}
