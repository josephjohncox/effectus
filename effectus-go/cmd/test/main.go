package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/schema/facts"
	"github.com/effectus/effectus-go/schema/registry"
	"github.com/effectus/effectus-go/unified"
	"github.com/effectus/effectus-go/util"
)

// SimpleExecutor is a basic implementation of effectus.Executor
type SimpleExecutor struct {
	Facts effectus.Facts
}

// Do implements the effectus.Executor interface
func (e *SimpleExecutor) Do(effect effectus.Effect) (interface{}, error) {
	fmt.Printf("Executing effect: %s\n", effect.Verb)

	// Resolve any fact references in the payload
	resolvedPayload := make(map[string]interface{})
	if payload, ok := effect.Payload.(map[string]interface{}); ok {
		for key, value := range payload {
			// Check if value is a fact path (string containing dots)
			if strValue, isStr := value.(string); isStr && strings.Contains(strValue, ".") {
				// Try to look it up in facts
				if factValue, exists := e.Facts.Get(strValue); exists {
					// Use the fact value instead
					fmt.Printf("  %s: %v (resolved from fact %s)\n", key, factValue, strValue)
					resolvedPayload[key] = factValue
					continue
				}
			}
			// Use the original value
			fmt.Printf("  %s: %v\n", key, value)
			resolvedPayload[key] = value
		}
	} else {
		fmt.Printf("  Payload: %v\n", effect.Payload)
	}

	// Return a mock result based on the verb
	switch effect.Verb {
	case "validateOrder":
		return true, nil
	case "processPayment":
		// Extract amount for the result
		var amount float64 = 0
		var customerId string = ""

		if val, ok := resolvedPayload["amount"]; ok {
			switch v := val.(type) {
			case float64:
				amount = v
			case int:
				amount = float64(v)
			case string:
				// Try to parse as float
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					amount = f
				}
			}
		}

		if val, ok := resolvedPayload["customerId"]; ok {
			if str, ok := val.(string); ok {
				customerId = str
			}
		}

		return map[string]interface{}{
			"status":     "success",
			"txid":       "txn_12345",
			"amount":     amount,
			"customerId": customerId,
		}, nil
	case "sendNotification":
		return "notification_sent", nil
	case "calculateTax":
		// Calculate tax based on amount and region
		var amount float64 = 0
		var region string = ""
		var taxRate float64 = 0.0

		if val, ok := resolvedPayload["amount"]; ok {
			if f, ok := val.(float64); ok {
				amount = f
			}
		}

		if val, ok := resolvedPayload["region"]; ok {
			if s, ok := val.(string); ok {
				region = s
				// Apply tax rate based on region
				if region == "US-CA" {
					taxRate = 0.0725
				} else {
					taxRate = 0.05
				}
			}
		}

		taxAmount := amount * taxRate

		return map[string]interface{}{
			"taxAmount": taxAmount,
			"taxRate":   taxRate,
			"region":    region,
		}, nil
	case "finalizeOrder":
		// Combine order, payment and tax info
		orderId := ""
		if val, ok := resolvedPayload["orderId"]; ok {
			if s, ok := val.(string); ok {
				orderId = s
			}
		}

		return map[string]interface{}{
			"orderId":   orderId,
			"status":    "finalized",
			"timestamp": time.Now().Format(time.RFC3339),
		}, nil
	case "logActivity":
		return "activity_logged", nil
	case "DoSomething":
		return "DoSomething", nil
	default:
		return nil, nil
	}
}

// SimpleFacts is a basic implementation of effectus.Facts
type SimpleFacts struct {
	Data map[string]interface{}
}

// Get implements the effectus.Facts interface
func (f *SimpleFacts) Get(path string) (interface{}, bool) {
	// First try direct lookup
	if value, ok := f.Data[path]; ok {
		return value, true
	}

	// If not found, handle dotted path navigation
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return nil, false
	}

	// For dotted paths, try to reconstruct the path with dots
	// This handles both formats: "customer.name" and "customername"
	current := ""
	for i, part := range parts {
		if i > 0 {
			current += "."
		}
		current += part

		if value, ok := f.Data[current]; ok {
			return value, true
		}
	}

	return nil, false
}

// Schema implements the effectus.Facts interface
func (f *SimpleFacts) Schema() effectus.SchemaInfo {
	return &facts.SimpleSchema{}
}

func main() {
	// Parse command line flags
	var mode string
	var verbose bool
	var execute bool
	var dumpAST bool

	flag.StringVar(&mode, "mode", "parse", "Mode to run in: parse, compile, or run")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
	flag.BoolVar(&execute, "execute", false, "Execute the compiled program")
	flag.BoolVar(&dumpAST, "ast", false, "Dump the AST structure")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Println("Usage: test [-mode=parse|compile|run] [-verbose] [-execute] [-ast] <file> [<file2> ...]")
		os.Exit(1)
	}

	// Get all file arguments
	filenames := flag.Args()

	if verbose {
		fmt.Printf("Processing %d file(s) in mode: %s\n", len(filenames), mode)
	}

	switch mode {
	case "parse":
		parseFiles(filenames, verbose, dumpAST)
	case "compile":
		compileFiles(filenames, verbose, dumpAST)
	case "run":
		runFiles(filenames, verbose, execute, dumpAST)
	default:
		fmt.Printf("Unknown mode: %s\n", mode)
		os.Exit(1)
	}
}

// parseFiles parses multiple rule files and displays their structure
func parseFiles(filenames []string, verbose bool, dumpAST bool) {
	for _, filename := range filenames {
		fmt.Printf("Parsing file: %s\n", filename)

		// Parse the file
		file, err := effectus.ParseFile(filename)
		if err != nil {
			fmt.Printf("Parser error for %s: %v\n", filename, err)
			continue
		}

		// Print the parsed file structure
		fmt.Printf("Successfully parsed %s!\n", filename)

		// Fix: AST File structure may have changed, check fields before accessing
		if dumpAST {
			dumpASTStructure(file)
		}
	}
}

// dumpASTStructure dumps the AST structure
func dumpASTStructure(file *ast.File) {
	dumper := util.NewStdoutASTDumper()
	dumper.DumpFile(file)
}

// compileFiles compiles multiple rule files using the appropriate compiler
func compileFiles(filenames []string, verbose bool, dumpAST bool) {
	// Group files by type
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
			fmt.Printf("Unsupported file extension for %s: %s (must be .eff or .effx)\n", path, ext)
		}
	}

	// Check if we have valid files to compile
	if len(effFiles) == 0 && len(effxFiles) == 0 {
		fmt.Println("No valid files to compile")
		os.Exit(1)
	}

	// Create a schema
	schema := &facts.SimpleSchema{}

	// Compile all files and merge them
	mergedSpec, err := compileAllFiles(effFiles, effxFiles, schema)
	if err != nil {
		fmt.Printf("Compilation error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully compiled %d files!\n", len(effFiles)+len(effxFiles))
	fmt.Printf("Required facts: %v\n", mergedSpec.RequiredFacts())

	if dumpAST && verbose {
		// Parse the files again to dump the AST
		for _, filename := range filenames {
			file, err := effectus.ParseFile(filename)
			if err == nil {
				fmt.Printf("\nAST for %s:\n", filename)
				dumpASTStructure(file)
			}
		}
	}
}

// compileAllFiles compiles both list and flow style rule files and merges them into a single spec
func compileAllFiles(effFiles, effxFiles []string, schema effectus.SchemaInfo) (effectus.Spec, error) {
	var listSpec *list.Spec
	var flowSpec *flow.Spec

	// Compile list-style (.eff) files if any
	if len(effFiles) > 0 {
		listCompiler := &list.Compiler{}
		var specs []effectus.Spec

		for _, path := range effFiles {
			spec, err := listCompiler.CompileFile(path, schema)
			if err != nil {
				return nil, fmt.Errorf("failed to compile %s: %w", path, err)
			}
			specs = append(specs, spec)
		}

		// Merge list specs
		listSpec = mergeListSpecs(specs)
	}

	// Compile flow-style (.effx) files if any
	if len(effxFiles) > 0 {
		flowCompiler := &flow.Compiler{}

		var specs []effectus.Spec
		for _, path := range effxFiles {
			spec, err := flowCompiler.CompileFile(path, schema)
			if err != nil {
				return nil, fmt.Errorf("failed to compile %s: %w", path, err)
			}
			specs = append(specs, spec)
		}

		// Merge flow specs
		flowSpec = mergeFlowSpecs(specs)
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
func mergeListSpecs(specs []effectus.Spec) *list.Spec {
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
func mergeFlowSpecs(specs []effectus.Spec) *flow.Spec {
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

// runFiles compiles and executes multiple rule files
func runFiles(filenames []string, verbose bool, execute bool, dumpAST bool) {
	// First compile the files
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
			fmt.Printf("Unsupported file extension for %s: %s (must be .eff or .effx)\n", path, ext)
		}
	}

	// Check if we have valid files to compile
	if len(effFiles) == 0 && len(effxFiles) == 0 {
		fmt.Println("No valid files to run")
		os.Exit(1)
	}

	// Create a schema
	schema := &facts.SimpleSchema{}

	// Compile all files and merge them
	mergedSpec, err := compileAllFiles(effFiles, effxFiles, schema)
	if err != nil {
		fmt.Printf("Compilation error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully compiled %d files!\n", len(effFiles)+len(effxFiles))

	if dumpAST {
		// Parse the files again to dump the AST
		for _, filename := range filenames {
			file, err := effectus.ParseFile(filename)
			if err == nil {
				fmt.Printf("\nAST for %s:\n", filename)
				dumpASTStructure(file)
			}
		}
	}

	if !execute {
		fmt.Println("Execution skipped (use -execute to run)")
		return
	}

	// Create sample facts
	facts := &SimpleFacts{
		Data: map[string]interface{}{
			// Dotted paths - these directly match the FactPath lexer token
			"customer.name":   "Example Customer",
			"customer.email":  "customer@example.com",
			"customer.id":     "CUST-12345",
			"customer.region": "US-CA",
			"order.id":        "ORD-54321",
			"order.total":     100.50,
			"order.items":     3,
			"test.value":      "test",
		},
	}

	if verbose {
		fmt.Println("Facts:", facts.Data)
	}

	// Create an executor
	executor := &SimpleExecutor{
		Facts: facts,
	}

	// Execute the spec
	ctx := context.Background()
	err = mergedSpec.Execute(ctx, facts, executor)
	if err != nil {
		fmt.Printf("Execution error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Execution completed successfully!")
}
