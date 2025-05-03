package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/schema"
)

var (
	typeCheck   = flag.Bool("typecheck", false, "Perform type checking on the input files")
	format      = flag.Bool("format", false, "Format the input files")
	output      = flag.String("output", "", "Output file for reports (defaults to stdout)")
	report      = flag.Bool("report", false, "Generate type report")
	verbose     = flag.Bool("verbose", false, "Show detailed output")
	compile     = flag.Bool("compile", false, "Compile files into a unified spec")
	schemaFiles = flag.String("schema", "", "Comma-separated list of schema files to load")
	verbSchemas = flag.String("verbschema", "", "Comma-separated list of verb schema files to load")
)

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: effectusc [options] <file1> [file2...]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Get all file arguments
	filenames := args

	if *verbose {
		fmt.Printf("Processing %d file(s)\n", len(filenames))
	}

	// Create a compiler
	comp := compiler.NewCompiler()

	// Load verb schemas if provided
	if *verbSchemas != "" {
		files := strings.Split(*verbSchemas, ",")
		for _, file := range files {
			if *verbose {
				fmt.Printf("Loading verb schemas from %s...\n", file)
			}
			err := comp.LoadVerbSpecs(file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading verb schema file %s: %v\n", file, err)
				continue
			}
		}
	}

	// Create facts for type checking
	facts, typeSystem := createEmptyFacts()

	// Explicitly register fact types with the compiler's type checker
	if *verbose {
		fmt.Println("Merging schema's type system with compiler's type checker")
	}

	// Get the compiler's type checker and merge our type system with it
	typeChecker := comp.GetTypeChecker()
	// Use the new method to directly merge type systems
	typeChecker.MergeTypeSystem(typeSystem)

	if *verbose {
		fmt.Println("Created test facts:")
		if schema, ok := facts.Schema().(*testSchema); ok {
			schema.DebugPrint()
		}
	}

	if *compile {
		// Compile all files into a unified spec
		spec, err := comp.ParseAndCompileFiles(filenames, facts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error compiling files: %v\n", err)
			os.Exit(1)
		}

		// Display information about the compiled spec
		printSpecInfo(spec)
	} else if *typeCheck || *report {
		typeCheckFiles(comp, filenames, facts)
	} else {
		parseFiles(comp, filenames)
	}
}

// parseFiles parses multiple files without type checking
func parseFiles(comp *compiler.Compiler, filenames []string) {
	for _, filename := range filenames {
		if *verbose {
			fmt.Printf("Parsing %s...\n", filename)
		}

		file, err := comp.ParseFile(filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", filename, err)
			continue
		}

		fmt.Printf("Successfully parsed %s: %d rules, %d flows\n",
			filename, len(file.Rules), len(file.Flows))
	}
}

// typeCheckFiles parses and type checks multiple files
func typeCheckFiles(comp *compiler.Compiler, filenames []string, facts effectus.Facts) {
	combinedReport := strings.Builder{}

	// Debug
	if *verbose {
		fmt.Println("Type checking facts contents:")
		if testFacts, ok := facts.(*testFacts); ok {
			fmt.Printf("  Schema: %+v\n", testFacts.Schema())
			fmt.Printf("  Facts map: %+v\n", testFacts.SimpleFacts)
		}
	}

	// Process all files
	for _, filename := range filenames {
		if *verbose {
			fmt.Printf("Processing %s...\n", filename)
		}

		// Parse and type check
		file, err := comp.ParseAndTypeCheck(filename, facts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", filename, err)
			continue
		}

		if *report {
			// Add file-specific report
			fileReport := fmt.Sprintf("# File: %s\n\n", filepath.Base(filename))
			fileReport += fmt.Sprintf("- Rules: %d\n", len(file.Rules))
			fileReport += fmt.Sprintf("- Flows: %d\n\n", len(file.Flows))
			combinedReport.WriteString(fileReport)
		} else {
			fmt.Printf("Successfully parsed and type-checked %s: %d rules, %d flows\n",
				filename, len(file.Rules), len(file.Flows))
		}
	}

	// If generating a report, append the type information
	if *report {
		// Generate and output type report
		typeReport := comp.GenerateTypeReport()
		combinedReport.WriteString(typeReport)

		report := combinedReport.String()
		outputReport(report)
	}
}

// printSpecInfo displays information about a compiled spec
func printSpecInfo(spec effectus.Spec) {
	fmt.Println("Compilation successful!")
	fmt.Printf("Required facts: %v\n", spec.RequiredFacts())

	// If we have a unified spec, show more details
	if unifiedSpec, ok := spec.(interface{ GetStats() map[string]int }); ok {
		stats := unifiedSpec.GetStats()
		fmt.Println("\nSpec details:")
		for key, value := range stats {
			fmt.Printf("- %s: %d\n", key, value)
		}
	}
}

// outputReport outputs the report to file or stdout
func outputReport(report string) {
	if *output != "" {
		err := os.WriteFile(*output, []byte(report), 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Report written to %s\n", *output)
	} else {
		fmt.Println(report)
	}
}

// createEmptyFacts creates an empty set of facts for type checking
func createEmptyFacts() (*testFacts, *schema.TypeSystem) {
	// Create a new type system for the schema
	typeSystem := schema.NewTypeSystem()

	// Load schema files if provided
	if *schemaFiles != "" {
		files := strings.Split(*schemaFiles, ",")
		for _, file := range files {
			if *verbose {
				fmt.Printf("Loading schema from %s...\n", file)
			}
			err := loadSchemaFile(typeSystem, file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading schema file %s: %v\n", file, err)
				continue
			}
		}

		// Debug - verify the schema was loaded correctly
		if *verbose {
			fmt.Printf("After loading schemas, type system has %d fact types\n", len(typeSystem.FactTypes))
		}
	}

	// Create a simple schema wrapper using the type system
	schemaInfo := &testSchema{typeSystem: typeSystem}

	if *verbose {
		fmt.Println("Schema info created, printing debug info:")
		schemaInfo.DebugPrint()
	}

	simpleFacts := schema.NewSimpleFacts(map[string]interface{}{}, schemaInfo)
	return &testFacts{SimpleFacts: simpleFacts}, typeSystem
}

// loadSchemaFile loads a schema file into the provided type system
func loadSchemaFile(typeSystem *schema.TypeSystem, filename string) error {
	// Read the file
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("reading schema file: %w", err)
	}

	// Parse the schema file
	var schemaEntries []struct {
		Path string      `json:"path"`
		Type schema.Type `json:"type"`
	}

	if err := json.Unmarshal(content, &schemaEntries); err != nil {
		return fmt.Errorf("parsing schema file: %w", err)
	}

	if *verbose {
		fmt.Printf("Found %d schema entries\n", len(schemaEntries))
	}

	// Add each entry to the type system
	for i, entry := range schemaEntries {
		if *verbose {
			fmt.Printf("Registering schema entry %d: path=%s, type=%v\n",
				i, entry.Path, entry.Type)
		}
		typeSystem.RegisterFactType(entry.Path, &entry.Type)
	}

	// Debug - print all registered fact types
	if *verbose {
		fmt.Println("Registered fact types:")
		for path, typ := range typeSystem.FactTypes {
			fmt.Printf("  %s: %v\n", path, typ)
		}
	}

	return nil
}

// testSchema implements the SchemaInfo interface using a TypeSystem
type testSchema struct {
	typeSystem *schema.TypeSystem
}

func (s *testSchema) ValidatePath(path string) bool {
	// Simple validation - in a real implementation, this would use the type system
	if path == "" {
		return false
	}

	if *verbose {
		fmt.Printf("SCHEMA VALIDATION: Checking path '%s'\n", path)
		// Print all registered paths for comparison
		fmt.Println("  All registered paths:")
		for registeredPath := range s.typeSystem.FactTypes {
			fmt.Printf("    - '%s'\n", registeredPath)
		}
	}

	// Check if the path exists in the type system
	typ, exists := s.typeSystem.GetFactType(path)

	if *verbose {
		fmt.Printf("SCHEMA VALIDATION RESULT: Path '%s' exists: %v", path, exists)
		if exists {
			fmt.Printf(", type: %v", typ)
		}
		fmt.Println()
	}

	return exists
}

// Debug method to print schema contents
func (s *testSchema) DebugPrint() {
	if s.typeSystem == nil {
		fmt.Println("testSchema: typeSystem is nil")
		return
	}

	fmt.Printf("testSchema: %d fact types registered\n", len(s.typeSystem.FactTypes))
	for path, typ := range s.typeSystem.FactTypes {
		fmt.Printf("  %s: %v\n", path, typ)
	}
}

// testFacts implements the Facts interface for the CLI tool
type testFacts struct {
	*schema.SimpleFacts
}

// Schema returns the schema information
func (f *testFacts) Schema() effectus.SchemaInfo {
	return f.SimpleFacts.Schema()
}

// Get returns the value at the given path
func (f *testFacts) Get(path string) (interface{}, bool) {
	return f.SimpleFacts.Get(path)
}
