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
	"github.com/effectus/effectus-go/schema"
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
	return &schema.SimpleSchema{}
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
		fmt.Println("Usage: test [-mode=parse|compile|run] [-verbose] [-execute] [-ast] <file>")
		os.Exit(1)
	}

	filename := flag.Arg(0)

	if verbose {
		fmt.Printf("Processing file: %s in mode: %s\n", filename, mode)
	}

	switch mode {
	case "parse":
		parseFile(filename, verbose, dumpAST)
	case "compile":
		compileFile(filename, verbose, dumpAST)
	case "run":
		runFile(filename, verbose, execute, dumpAST)
	default:
		fmt.Printf("Unknown mode: %s\n", mode)
		os.Exit(1)
	}
}

// parseFile parses a rule file and displays its structure
func parseFile(filename string, verbose bool, dumpAST bool) {
	// Parse the file
	file, err := effectus.ParseFile(filename)
	if err != nil {
		fmt.Printf("Parser error: %v\n", err)
		os.Exit(1)
	}

	// Print the parsed file structure
	fmt.Printf("Successfully parsed %s!\n", filename)
	fmt.Printf("Includes: %d\n", len(file.Includes))
	fmt.Printf("Rules: %d\n", len(file.Rules))
	fmt.Printf("Flows: %d\n", len(file.Flows))

	if verbose || dumpAST {
		dumpASTStructure(file)
	}
}

// dumpASTStructure dumps the AST structure
func dumpASTStructure(file *ast.File) {
	if len(file.Flows) > 0 {
		flow := file.Flows[0]
		fmt.Printf("\nFlow Details:\n")
		fmt.Printf("  Name: %s\n", flow.Name)
		fmt.Printf("  Priority: %d\n", flow.Priority)

		if flow.When != nil && flow.When.Predicates != nil {
			fmt.Printf("  Predicates: %d\n", len(flow.When.Predicates))
			for i, pred := range flow.When.Predicates {
				fmt.Printf("    Predicate %d: %s %s\n", i+1, pred.Path, pred.Op)
				if pred.Lit.String != nil {
					fmt.Printf("      Compare with string: %s\n", *pred.Lit.String)
				} else if pred.Lit.Int != nil {
					fmt.Printf("      Compare with int: %d\n", *pred.Lit.Int)
				} else if pred.Lit.Float != nil {
					fmt.Printf("      Compare with float: %f\n", *pred.Lit.Float)
				}
			}
		}

		if flow.Steps != nil && flow.Steps.Steps != nil {
			fmt.Printf("  Steps: %d\n", len(flow.Steps.Steps))

			for i, step := range flow.Steps.Steps {
				fmt.Printf("    Step %d: %s\n", i+1, step.Verb)
				fmt.Printf("      Args: %d\n", len(step.Args))
				for j, arg := range step.Args {
					fmt.Printf("        Arg %d: %s\n", j+1, arg.Name)
					if arg.Value != nil {
						if arg.Value.VarRef != "" {
							fmt.Printf("          VarRef: %s\n", arg.Value.VarRef)
						} else if arg.Value.FactPath != "" {
							fmt.Printf("          FactPath: %s\n", arg.Value.FactPath)
						} else if arg.Value.Literal != nil {
							fmt.Printf("          Literal: %v\n", describeLiteral(arg.Value.Literal))
						}
					}
				}
				if step.BindName != "" {
					fmt.Printf("      BindName: %s\n", step.BindName)
				}
			}
		}
	}
}

// Helper function to describe literals nicely
func describeLiteral(lit *ast.Literal) string {
	if lit.String != nil {
		return fmt.Sprintf("string(%s)", *lit.String)
	}
	if lit.Int != nil {
		return fmt.Sprintf("int(%d)", *lit.Int)
	}
	if lit.Float != nil {
		return fmt.Sprintf("float(%f)", *lit.Float)
	}
	if lit.Bool != nil {
		return fmt.Sprintf("bool(%t)", *lit.Bool)
	}
	return "unknown"
}

// compileFile compiles a rule file using the appropriate compiler
func compileFile(filename string, verbose bool, dumpAST bool) {
	// Determine the appropriate compiler based on file extension
	ext := filepath.Ext(filename)
	var compiler effectus.Compiler

	switch ext {
	case ".eff":
		fmt.Println("Using list-style compiler")
		// In a complete implementation, we would use:
		// compiler = &list.Compiler{}
		fmt.Println("List compiler not implemented in this demo")
		os.Exit(1)
	case ".effx":
		fmt.Println("Using flow-style compiler")
		compiler = &flow.Compiler{}
	default:
		fmt.Printf("Unsupported file extension: %s\n", ext)
		os.Exit(1)
	}

	// Create a schema
	schema := &schema.SimpleSchema{}

	// Compile the file
	spec, err := compiler.CompileFile(filename, schema)
	if err != nil {
		fmt.Printf("Compilation error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully compiled %s!\n", filename)
	fmt.Printf("Required facts: %v\n", spec.RequiredFacts())

	if dumpAST {
		// Parse the file again to dump the AST
		file, err := effectus.ParseFile(filename)
		if err == nil {
			dumpASTStructure(file)
		}
	}
}

// runFile compiles and executes a rule file
func runFile(filename string, verbose bool, execute bool, dumpAST bool) {
	// Compile the file first
	ext := filepath.Ext(filename)
	var compiler effectus.Compiler

	switch ext {
	case ".eff":
		fmt.Println("Using list-style compiler")
		// In a complete implementation, we would use:
		// compiler = &list.Compiler{}
		fmt.Println("List compiler not implemented in this demo")
		os.Exit(1)
	case ".effx":
		fmt.Println("Using flow-style compiler")
		compiler = &flow.Compiler{}
	default:
		fmt.Printf("Unsupported file extension: %s\n", ext)
		os.Exit(1)
	}

	// Create a schema
	schema := &schema.SimpleSchema{}

	// Compile the file
	spec, err := compiler.CompileFile(filename, schema)
	if err != nil {
		fmt.Printf("Compilation error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully compiled %s!\n", filename)

	if dumpAST {
		// Parse the file again to dump the AST
		file, err := effectus.ParseFile(filename)
		if err == nil {
			dumpASTStructure(file)
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
	err = spec.Execute(ctx, facts, executor)
	if err != nil {
		fmt.Printf("Execution error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Execution completed successfully!")
}
