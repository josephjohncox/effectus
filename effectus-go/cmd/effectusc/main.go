package main

import (
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
	typeCheck = flag.Bool("typecheck", false, "Perform type checking on the input files")
	format    = flag.Bool("format", false, "Format the input files")
	output    = flag.String("output", "", "Output file for reports (defaults to stdout)")
	report    = flag.Bool("report", false, "Generate type report")
	verbose   = flag.Bool("verbose", false, "Show detailed output")
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

	if *typeCheck || *report {
		typeCheckFiles(filenames)
	} else {
		parseFiles(filenames)
	}
}

// parseFiles parses multiple files without type checking
func parseFiles(filenames []string) {
	comp := compiler.NewCompiler()

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
func typeCheckFiles(filenames []string) {
	comp := compiler.NewCompiler()
	combinedReport := strings.Builder{}

	// Create empty facts for now
	facts := createEmptyFacts()

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
}

// createEmptyFacts creates an empty set of facts for type checking
func createEmptyFacts() *testFacts {
	schemaInfo := &schema.SimpleSchema{}
	simpleFacts := schema.NewSimpleFacts(map[string]interface{}{}, schemaInfo)
	return &testFacts{SimpleFacts: simpleFacts}
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
