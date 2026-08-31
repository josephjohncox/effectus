package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/josephjohncox/effectus"
	"github.com/josephjohncox/effectus/ast"
	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/util"
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
		param1 := ""
		if val, ok := resolvedPayload["param1"]; ok {
			if s, ok := val.(string); ok {
				param1 = s
			}
		}
		fmt.Printf("validateOrder called with param1: %s\n", param1)
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
		msg := "default_message"
		if val, ok := resolvedPayload["msg"]; ok {
			if s, ok := val.(string); ok {
				msg = s
			}
		}
		fmt.Printf("DoSomething called with message: %s\n", msg)
		return msg, nil
	default:
		fmt.Printf("Unknown verb: %s, with payload: %v\n", effect.Verb, resolvedPayload)
		return nil, nil
	}
}

// SimpleFacts is a basic implementation of effectus.Facts
type SimpleFacts struct {
	data       map[string]interface{}
	schemaInfo effectus.SchemaInfo
}

// NewSimpleFacts creates a new SimpleFacts instance
func NewSimpleFacts(data map[string]interface{}) *SimpleFacts {
	return &SimpleFacts{
		data:       data,
		schemaInfo: &SimpleSchema{},
	}
}

// Get implements the effectus.Facts interface
func (f *SimpleFacts) Get(path string) (interface{}, bool) {
	// First try direct lookup
	if value, ok := f.data[path]; ok {
		return value, true
	}

	return nil, false
}

// Schema implements the effectus.Facts interface
func (f *SimpleFacts) Schema() effectus.SchemaInfo {
	return f.schemaInfo
}

// SimpleSchema is a basic implementation of SchemaInfo
type SimpleSchema struct{}

// ValidatePath implements the SchemaInfo interface
func (s *SimpleSchema) ValidatePath(path string) bool {
	// Simple implementation that accepts all paths
	return true
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
	comp := compiler.NewCompiler()

	for _, filename := range filenames {
		fmt.Printf("Parsing file: %s\n", filename)

		// Parse the file
		file, err := comp.ParseFile(filename)
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

// compileFiles compiles multiple rule files using the public compatibility API.
func compileFiles(filenames []string, verbose bool, dumpAST bool) {
	mergedSpec, err := compileProgram(filenames)
	if err != nil {
		fmt.Printf("Compilation error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully compiled %d files!\n", len(filenames))
	fmt.Printf("Required facts: %v\n", mergedSpec.RequiredFacts())

	if dumpAST && verbose {
		comp := compiler.NewCompiler()
		for _, filename := range filenames {
			file, err := comp.ParseFile(filename)
			if err == nil {
				fmt.Printf("\nAST for %s:\n", filename)
				dumpASTStructure(file)
			}
		}
	}
}

func compileProgram(filenames []string) (*compiler.CompiledSpec, error) {
	comp := compiler.NewCompiler()
	return comp.CompileUncheckedProgram(filenames, NewSimpleFacts(nil))
}

// runFiles compiles and executes multiple rule files
func runFiles(filenames []string, verbose bool, execute bool, dumpAST bool) {
	mergedSpec, err := compileProgram(filenames)
	if err != nil {
		fmt.Printf("Compilation error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully compiled %d files!\n", len(filenames))

	if dumpAST {
		// Create a compiler for parsing
		comp := compiler.NewCompiler()

		// Parse the files again to dump the AST
		for _, filename := range filenames {
			file, err := comp.ParseFile(filename)
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
	facts := NewSimpleFacts(map[string]interface{}{
		// Dotted paths - these directly match the FactPath lexer token
		"customer.name":   "Example Customer",
		"customer.email":  "customer@example.com",
		"customer.id":     "CUST-12345",
		"customer.region": "US-CA",
		"order.id":        "ORD-54321",
		"order.total":     100.50,
		"order.items":     3,
		"test.value":      "test",
	})

	if verbose {
		fmt.Println("Facts:", facts.data)
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
