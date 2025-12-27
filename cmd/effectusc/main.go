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
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/effectus/effectus-go/unified"
)

// Command represents a sub-command of effectusc
type Command struct {
	Name        string
	Description string
	FlagSet     *flag.FlagSet
	Run         func() error
}

var (
	// Global flags
	verbose = flag.Bool("verbose", false, "Show detailed output")

	// Command-specific flags - these will be re-defined for each command
	commands = make(map[string]*Command)
)

func main() {
	// Define commands
	defineCommands()

	// Check if a command was provided
	flag.Parse()
	args := flag.Args()

	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: effectusc <command> [options]")
		fmt.Fprintln(os.Stderr, "Available commands:")
		for name, cmd := range commands {
			fmt.Fprintf(os.Stderr, "  %s\t%s\n", name, cmd.Description)
		}
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Get the command
	cmdName := args[0]
	cmd, ok := commands[cmdName]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmdName)
		fmt.Fprintln(os.Stderr, "Available commands:")
		for name, cmd := range commands {
			fmt.Fprintf(os.Stderr, "  %s\t%s\n", name, cmd.Description)
		}
		os.Exit(1)
	}

	// Parse command-specific flags
	cmd.FlagSet.Parse(args[1:])

	// Run the command
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func defineCommands() {
	// Define typecheck command
	typeCheckCmd := &Command{
		Name:        "typecheck",
		Description: "Type check rule files",
		FlagSet:     flag.NewFlagSet("typecheck", flag.ExitOnError),
	}

	tcSchemaFiles := typeCheckCmd.FlagSet.String("schema", "", "Comma-separated list of schema files to load")
	tcVerbSchemas := typeCheckCmd.FlagSet.String("verbschema", "", "Comma-separated list of verb schema files to load")
	tcOutput := typeCheckCmd.FlagSet.String("output", "", "Output file for reports (defaults to stdout)")
	tcReport := typeCheckCmd.FlagSet.Bool("report", false, "Generate type report")
	tcVerbose := typeCheckCmd.FlagSet.Bool("verbose", false, "Show detailed output")

	typeCheckCmd.Run = func() error {
		// Get file arguments
		files := typeCheckCmd.FlagSet.Args()
		if len(files) < 1 {
			return fmt.Errorf("no input files specified")
		}

		if *tcVerbose {
			fmt.Printf("Processing %d file(s) for type checking\n", len(files))
		}

		// Create a compiler
		comp := compiler.NewCompiler()

		// Load verb schemas if provided
		if *tcVerbSchemas != "" {
			files := strings.Split(*tcVerbSchemas, ",")
			for _, file := range files {
				if *tcVerbose {
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
		facts, typeSystem := createEmptyFacts(*tcSchemaFiles, *tcVerbose)

		// Get the compiler's type checker and merge our type system with it
		typeChecker := comp.GetTypeSystem()
		typeChecker.MergeTypeSystem(typeSystem)

		// Process all files
		combinedReport := strings.Builder{}
		for _, filename := range files {
			if *tcVerbose {
				fmt.Printf("Processing %s...\n", filename)
			}

			// Parse and type check
			file, err := comp.ParseAndTypeCheck(filename, facts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", filename, err)
				continue
			}

			if *tcReport {
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
		if *tcReport {
			// Generate and output type report
			typeReport := comp.GenerateTypeReport()
			combinedReport.WriteString(typeReport)

			report := combinedReport.String()
			outputReport(report, *tcOutput)
		}

		return nil
	}

	// Define compile command
	compileCmd := &Command{
		Name:        "compile",
		Description: "Compile files into a unified spec",
		FlagSet:     flag.NewFlagSet("compile", flag.ExitOnError),
	}

	cSchemaFiles := compileCmd.FlagSet.String("schema", "", "Comma-separated list of schema files to load")
	cVerbSchemas := compileCmd.FlagSet.String("verbschema", "", "Comma-separated list of verb schema files to load")
	cOutput := compileCmd.FlagSet.String("output", "spec.json", "Output file for compiled spec")
	cVerbose := compileCmd.FlagSet.Bool("verbose", false, "Show detailed output")

	compileCmd.Run = func() error {
		// Get file arguments
		files := compileCmd.FlagSet.Args()
		if len(files) < 1 {
			return fmt.Errorf("no input files specified")
		}

		if *cVerbose {
			fmt.Printf("Compiling %d file(s)\n", len(files))
		}

		// Create a compiler
		comp := compiler.NewCompiler()

		// Load verb schemas if provided
		if *cVerbSchemas != "" {
			files := strings.Split(*cVerbSchemas, ",")
			for _, file := range files {
				if *cVerbose {
					fmt.Printf("Loading verb schemas from %s...\n", file)
				}
				err := comp.LoadVerbSpecs(file)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error loading verb schema file %s: %v\n", file, err)
					continue
				}
			}
		}

		// Create facts for compilation
		facts, typeSystem := createEmptyFacts(*cSchemaFiles, *cVerbose)

		// Get the compiler's type checker and merge our type system with it
		typeChecker := comp.GetTypeSystem()
		typeChecker.MergeTypeSystem(typeSystem)

		// Compile all files into a unified spec
		spec, err := comp.ParseAndCompileFiles(files, facts)
		if err != nil {
			return fmt.Errorf("compiling files: %w", err)
		}

		// Display information about the compiled spec
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

		// Save the spec to file
		specJSON, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling spec: %w", err)
		}

		if err := os.WriteFile(*cOutput, specJSON, 0644); err != nil {
			return fmt.Errorf("writing spec to %s: %w", *cOutput, err)
		}

		fmt.Printf("Spec written to %s\n", *cOutput)
		return nil
	}

	// Define bundle command
	bundleCmd := &Command{
		Name:        "bundle",
		Description: "Create a bundle from schema, verbs, and rules",
		FlagSet:     flag.NewFlagSet("bundle", flag.ExitOnError),
	}

	bName := bundleCmd.FlagSet.String("name", "", "Bundle name")
	bVersion := bundleCmd.FlagSet.String("version", "1.0.0", "Bundle version")
	bDesc := bundleCmd.FlagSet.String("desc", "", "Bundle description")
	bSchemaDir := bundleCmd.FlagSet.String("schema-dir", "", "Directory containing schema files")
	bVerbDir := bundleCmd.FlagSet.String("verb-dir", "", "Directory containing verb files")
	bRulesDir := bundleCmd.FlagSet.String("rules-dir", "", "Directory containing rule files")
	bOutput := bundleCmd.FlagSet.String("output", "bundle.json", "Output file for bundle")
	bOciRef := bundleCmd.FlagSet.String("oci-ref", "", "OCI reference to push bundle to (e.g., ghcr.io/user/bundle:v1)")
	bPiiMasks := bundleCmd.FlagSet.String("pii-masks", "", "Comma-separated list of PII paths to mask")
	bVerbose := bundleCmd.FlagSet.Bool("verbose", false, "Show detailed output")

	bundleCmd.Run = func() error {
		if *bName == "" {
			return fmt.Errorf("bundle name is required")
		}

		// Validate required directories
		if *bSchemaDir == "" && *bVerbDir == "" && *bRulesDir == "" {
			return fmt.Errorf("at least one of schema-dir, verb-dir, or rules-dir must be specified")
		}

		// Create a bundle builder
		builder := unified.NewBundleBuilder(*bName, *bVersion)
		builder.WithDescription(*bDesc)

		if *bSchemaDir != "" {
			if *bVerbose {
				fmt.Printf("Using schema directory: %s\n", *bSchemaDir)
			}
			builder.WithSchemaDir(*bSchemaDir)
		}

		if *bVerbDir != "" {
			if *bVerbose {
				fmt.Printf("Using verb directory: %s\n", *bVerbDir)
			}
			builder.WithVerbDir(*bVerbDir)
		}

		if *bRulesDir != "" {
			if *bVerbose {
				fmt.Printf("Using rules directory: %s\n", *bRulesDir)
			}
			builder.WithRulesDir(*bRulesDir)
		}

		// Add PII masks if specified
		if *bPiiMasks != "" {
			masks := strings.Split(*bPiiMasks, ",")
			if *bVerbose {
				fmt.Printf("Adding %d PII masks\n", len(masks))
			}
			builder.WithPIIMasks(masks)
		}

		// Build the bundle
		bundle, err := builder.Build()
		if err != nil {
			return fmt.Errorf("building bundle: %w", err)
		}

		// Show bundle info
		fmt.Printf("Created bundle: %s v%s\n", bundle.Name, bundle.Version)
		fmt.Printf("Schema files: %d\n", len(bundle.SchemaFiles))
		fmt.Printf("Verb files: %d\n", len(bundle.VerbFiles))
		fmt.Printf("Rule files: %d\n", len(bundle.RuleFiles))

		// Save the bundle
		if err := unified.SaveBundle(bundle, *bOutput); err != nil {
			return fmt.Errorf("saving bundle to %s: %w", *bOutput, err)
		}
		fmt.Printf("Bundle saved to %s\n", *bOutput)

		// Push to OCI registry if specified
		if *bOciRef != "" {
			if *bVerbose {
				fmt.Printf("Pushing bundle to %s\n", *bOciRef)
			}

			pusher := unified.NewOCIBundlePusher(bundle)

			if *bSchemaDir != "" {
				pusher.WithSchemaDir(*bSchemaDir)
			}

			if *bVerbDir != "" {
				pusher.WithVerbDir(*bVerbDir)
			}

			if *bRulesDir != "" {
				pusher.WithRulesDir(*bRulesDir)
			}

			if err := pusher.Push(*bOciRef); err != nil {
				return fmt.Errorf("pushing bundle to %s: %w", *bOciRef, err)
			}

			fmt.Printf("Bundle pushed to %s\n", *bOciRef)
		}

		return nil
	}

	// Define parse command
	parseCmd := &Command{
		Name:        "parse",
		Description: "Parse rule files without type checking",
		FlagSet:     flag.NewFlagSet("parse", flag.ExitOnError),
	}

	pVerbose := parseCmd.FlagSet.Bool("verbose", false, "Show detailed output")

	parseCmd.Run = func() error {
		// Get file arguments
		files := parseCmd.FlagSet.Args()
		if len(files) < 1 {
			return fmt.Errorf("no input files specified")
		}

		if *pVerbose {
			fmt.Printf("Parsing %d file(s)\n", len(files))
		}

		// Create a compiler
		comp := compiler.NewCompiler()

		// Parse each file
		for _, filename := range files {
			if *pVerbose {
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

		return nil
	}

	// Define capabilities command
	capabilitiesCmd := &Command{
		Name:        "capabilities",
		Description: "Analyze verb capabilities in rule files",
		FlagSet:     flag.NewFlagSet("capabilities", flag.ExitOnError),
	}

	capOutput := capabilitiesCmd.FlagSet.String("output", "", "Output file for analysis report (defaults to stdout)")
	capVerbose := capabilitiesCmd.FlagSet.Bool("verbose", false, "Show detailed output")

	capabilitiesCmd.Run = func() error {
		// Get file arguments
		files := capabilitiesCmd.FlagSet.Args()
		if len(files) < 1 {
			return fmt.Errorf("no input files specified")
		}

		if *capVerbose {
			fmt.Printf("Analyzing capabilities in %d file(s)\n", len(files))
		}

		// Create verb registry (defaults only unless user-provided specs are loaded)
		registry := verb.NewRegistry(nil)
		_ = registry.RegisterDefaults()

		analyzer := verb.NewCapabilityAnalyzer(registry)

		// Combined analysis results for all files
		combinedReport := strings.Builder{}

		// Process each file
		for _, filename := range files {
			if *capVerbose {
				fmt.Printf("Analyzing capabilities in %s...\n", filename)
			}

			// Parse the file
			comp := compiler.NewCompiler()
			file, err := comp.ParseFile(filename)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing file %s: %v\n", filename, err)
				continue
			}

			// Analyze capabilities
			result, err := analyzer.Analyze(file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error analyzing file %s: %v\n", filename, err)
				continue
			}

			// Add file-specific report
			fileReport := fmt.Sprintf("## Capability Analysis: %s\n\n", filepath.Base(filename))
			fileReport += verb.FormatAnalysisResult(result)
			fileReport += "\n\n"

			combinedReport.WriteString(fileReport)

			if *capVerbose {
				fmt.Printf("Analysis complete for %s\n", filename)
			}
		}

		// Output the report
		report := combinedReport.String()
		outputReport(report, *capOutput)

		return nil
	}

	// Register commands
	commands["typecheck"] = typeCheckCmd
	commands["compile"] = compileCmd
	commands["bundle"] = bundleCmd
	commands["parse"] = parseCmd
	commands["capabilities"] = capabilitiesCmd
	commands["check"] = newCheckCommand()
	commands["lsp"] = newLSPCommand()
	commands["graph"] = newGraphCommand()
	commands["format"] = newFormatCommand()
	commands["resolve"] = newResolveCommand()
}

// outputReport outputs the report to file or stdout
func outputReport(report string, output string) {
	if output != "" {
		err := os.WriteFile(output, []byte(report), 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Report written to %s\n", output)
	} else {
		fmt.Println(report)
	}
}

// createEmptyFacts creates an empty set of facts for type checking
func createEmptyFacts(schemaFiles string, verbose bool) (*testFacts, *types.TypeSystem) {
	// Create a new type system
	typeSystem := types.NewTypeSystem()

	// Load schema files if provided
	if schemaFiles != "" {
		files := expandSchemaPaths(strings.Split(schemaFiles, ","))
		for _, file := range files {
			if verbose {
				fmt.Printf("Loading schema from %s...\n", file)
			}
			if err := typeSystem.LoadSchemaFile(file); err != nil {
				if jsonErr := typeSystem.LoadJSONSchemaFile(file); jsonErr != nil {
					fmt.Fprintf(os.Stderr, "Error loading schema file %s: %v\n", file, err)
					continue
				}
			}
		}

		// Debug - verify the schema was loaded correctly
		if verbose {
			paths := typeSystem.GetAllFactPaths()
			fmt.Printf("After loading schemas, type system has %d fact types\n", len(paths))
		}
	}

	// Create a simple schema wrapper using the type system
	schemaInfo := &testSchema{typeSystem: typeSystem}

	if verbose {
		fmt.Println("Schema info created, printing debug info:")
		schemaInfo.DebugPrint()
	}

	// Create a provider with empty data
	provider := pathutil.NewRegistryFactProviderFromMap(map[string]interface{}{})

	// Create a registry to manage namespaces
	registry := pathutil.NewRegistry()
	registry.Register("", provider) // Register at root

	return &testFacts{factRegistry: registry, schema: schemaInfo}, typeSystem
}

func expandSchemaPaths(paths []string) []string {
	expanded := make([]string, 0)
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			expanded = append(expanded, path)
			continue
		}

		if info.IsDir() {
			entries, err := os.ReadDir(path)
			if err != nil {
				expanded = append(expanded, path)
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if strings.HasSuffix(entry.Name(), ".json") {
					expanded = append(expanded, filepath.Join(path, entry.Name()))
				}
			}
			continue
		}

		expanded = append(expanded, path)
	}

	return expanded
}

// loadSchemaFile loads a schema file into the provided type system
func loadSchemaFile(typeSystem *types.TypeSystem, filename string, verbose bool) error {
	// Read the file
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("reading schema file: %w", err)
	}

	// Parse the schema file
	var schemaEntries []struct {
		Path string     `json:"path"`
		Type types.Type `json:"type"`
	}

	if err := json.Unmarshal(content, &schemaEntries); err != nil {
		return fmt.Errorf("parsing schema file: %w", err)
	}

	if verbose {
		fmt.Printf("Found %d schema entries\n", len(schemaEntries))
	}

	// Add each entry to the type system
	for i, entry := range schemaEntries {
		if verbose {
			fmt.Printf("Registering schema entry %d: path=%s, type=%v\n",
				i, entry.Path, entry.Type)
		}
		// No need to parse path string anymore, just use it directly
		typeSystem.RegisterFactType(entry.Path, &entry.Type)
	}

	// Debug - print all registered fact types
	if verbose {
		fmt.Println("Registered fact types:")
		paths := typeSystem.GetAllFactPaths()
		for _, path := range paths {
			typ, _ := typeSystem.GetFactType(path)
			fmt.Printf("  %s: %v\n", path, typ)
		}
	}

	return nil
}

// testSchema implements the SchemaInfo interface using a TypeSystem
type testSchema struct {
	typeSystem *types.TypeSystem
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
		paths := s.typeSystem.GetAllFactPaths()
		for _, registeredPath := range paths {
			fmt.Printf("    - '%s'\n", registeredPath)
		}
	}

	// Check if the path exists in the type system
	typ, err := s.typeSystem.GetFactType(path)
	exists := err == nil

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

	paths := s.typeSystem.GetAllFactPaths()
	fmt.Printf("testSchema: %d fact types registered\n", len(paths))
	for _, path := range paths {
		typ, _ := s.typeSystem.GetFactType(path)
		fmt.Printf("  %s: %v\n", path, typ)
	}
}

// testFacts implements the Facts interface for the CLI tool
type testFacts struct {
	factRegistry *pathutil.Registry
	schema       *testSchema
}

// Get retrieves a fact by its path
func (f *testFacts) Get(path string) (interface{}, bool) {
	return f.factRegistry.Get(path)
}

// Has checks if a fact exists by its path
func (f *testFacts) Has(path string) bool {
	_, exists := f.Get(path)
	return exists
}

// Schema returns the schema information
func (f *testFacts) Schema() effectus.SchemaInfo {
	return f.schema
}

// Type returns the type of a fact (not implemented)
func (f *testFacts) Type(path string) interface{} {
	return nil
}
