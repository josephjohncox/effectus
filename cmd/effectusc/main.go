package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/josephjohncox/effectus"
	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/internal/schemasources"
	"github.com/josephjohncox/effectus/lint"
	"github.com/josephjohncox/effectus/pathutil"
	"github.com/josephjohncox/effectus/schema/types"
	"github.com/josephjohncox/effectus/schema/verb"
	"github.com/josephjohncox/effectus/unified"
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
		for _, name := range sortedCommandNames() {
			fmt.Fprintf(os.Stderr, "  %s\t%s\n", name, commands[name].Description)
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
		for _, name := range sortedCommandNames() {
			fmt.Fprintf(os.Stderr, "  %s\t%s\n", name, commands[name].Description)
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

func sortedCommandNames() []string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func defineCommands() {
	// Define typecheck command
	typeCheckCmd := &Command{
		Name:        "typecheck",
		Description: "Type check rule files",
		FlagSet:     flag.NewFlagSet("typecheck", flag.ExitOnError),
	}

	tcSchemaFiles := typeCheckCmd.FlagSet.String("schema", "", "Comma-separated list of schema files to load")
	tcSchemaSources := typeCheckCmd.FlagSet.String("schema-sources", "", "Path to schema sources config (YAML/JSON)")
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

		var failures []error

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
					failures = append(failures, fmt.Errorf("load verb schema %s: %w", file, err))
					continue
				}
			}
		}

		// Create facts for type checking
		facts, typeSystem, err := createEmptyFacts(*tcSchemaFiles, *tcVerbose)
		if err != nil {
			failures = append(failures, err)
		}
		if strings.TrimSpace(*tcSchemaSources) != "" {
			sources, err := schemasources.LoadFromFile(*tcSchemaSources)
			if err != nil {
				failures = append(failures, err)
			} else if err := schemasources.Apply(context.Background(), typeSystem, sources, *tcVerbose); err != nil {
				failures = append(failures, err)
			}
		}

		if err := errors.Join(failures...); err != nil {
			return err
		}

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
				failures = append(failures, fmt.Errorf("process %s: %w", filename, err))
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

		return errors.Join(failures...)
	}

	// Define compile command
	compileCmd := &Command{
		Name:        "compile",
		Description: "Compile files into a unified spec",
		FlagSet:     flag.NewFlagSet("compile", flag.ExitOnError),
	}

	cSchemaFiles := compileCmd.FlagSet.String("schema", "", "Comma-separated list of schema files to load")
	cSchemaSources := compileCmd.FlagSet.String("schema-sources", "", "Path to schema sources config (YAML/JSON)")
	cVerbSchemas := compileCmd.FlagSet.String("verbschema", "", "Comma-separated list of verb schema files to load")
	cOutput := compileCmd.FlagSet.String("output", "rules.effir", "Output file for checked IR artifact")
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

		verbFiles := splitCommaList(*cVerbSchemas)
		registry, verbErr := loadVerbRegistry(verbFiles, *cVerbose)

		// Create declarations for checked compilation. Load every explicit input
		// before returning so users receive all path failures in one invocation.
		_, typeSystem, schemaErr := createEmptyFacts(*cSchemaFiles, *cVerbose)
		var sourceErr error
		if strings.TrimSpace(*cSchemaSources) != "" {
			schemaSourceDeclarations, err := schemasources.LoadFromFile(*cSchemaSources)
			if err != nil {
				sourceErr = err
			} else if err := schemasources.Apply(context.Background(), typeSystem, schemaSourceDeclarations, *cVerbose); err != nil {
				sourceErr = err
			}
		}
		if err := errors.Join(verbErr, schemaErr, sourceErr); err != nil {
			return err
		}

		environment, err := compiler.BuildIREnvironment(typeSystem, registry)
		if err != nil {
			return err
		}
		sources, err := compiler.LoadSources(files)
		if err != nil {
			return err
		}
		checked, err := compiler.CompileChecked(context.Background(), sources, environment, compiler.CompileOptions{})
		if err != nil {
			return fmt.Errorf("compiling files: %w", err)
		}
		artifactBytes := checked.Marshal()

		fmt.Println("Compilation successful!")
		if err := os.WriteFile(*cOutput, artifactBytes, 0644); err != nil {
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
	bSchemaSources := bundleCmd.FlagSet.String("schema-sources", "", "Path to schema sources config (YAML/JSON)")
	bVerbDir := bundleCmd.FlagSet.String("verb-dir", "", "Directory containing verb files")
	bVerbSchemas := bundleCmd.FlagSet.String("verbschema", "", "Comma-separated list of verb schema files to load")
	bRulesDir := bundleCmd.FlagSet.String("rules-dir", "", "Directory containing rule files")
	bCheck := bundleCmd.FlagSet.Bool("check", true, "Run math/semantic checks before bundling")
	bUnsafe := bundleCmd.FlagSet.String("unsafe", "error", "Unsafe expression policy: warn, error, ignore")
	bVerbMode := bundleCmd.FlagSet.String("verbs", "error", "Verb lint policy: error, warn, ignore")
	bFailOnWarn := bundleCmd.FlagSet.Bool("fail-on-warn", false, "Return non-zero exit code when warnings are present")
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

		if *bVerbSchemas != "" {
			paths := expandSchemaPaths(strings.Split(*bVerbSchemas, ","))
			verbSpecFiles := make([]string, 0, len(paths))
			for _, path := range paths {
				if filepath.Ext(path) == ".json" {
					verbSpecFiles = append(verbSpecFiles, path)
				}
			}
			if *bVerbose {
				fmt.Printf("Loading %d verb spec files\n", len(verbSpecFiles))
			}
			builder.WithVerbSpecFiles(verbSpecFiles)
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

		if *bCheck {
			ruleFiles, err := collectRuleFiles(*bRulesDir)
			if err != nil {
				return fmt.Errorf("collecting rule files: %w", err)
			}
			if len(ruleFiles) > 0 {
				unsafeMode, err := lint.ParseUnsafeMode(*bUnsafe)
				if err != nil {
					return err
				}
				verbMode, err := lint.ParseVerbMode(*bVerbMode)
				if err != nil {
					return err
				}

				var registry *verb.Registry
				if *bVerbDir != "" {
					verbFiles := expandSchemaPaths([]string{*bVerbDir})
					registry, err = loadVerbRegistry(verbFiles, *bVerbose)
					if err != nil {
						return err
					}
				}

				if verbMode != lint.VerbIgnore && registry == nil {
					return fmt.Errorf("verb linting enabled but no verb registry loaded; provide --verb-dir or set --verbs=ignore")
				}

				issues, hadWarn, hadError, err := runCheck(runCheckOptions{
					files:       ruleFiles,
					schemaFiles: *bSchemaDir,
					registry:    registry,
					lintOptions: lint.LintOptions{
						UnsafeMode: unsafeMode,
						VerbMode:   verbMode,
					},
					verbose:       *bVerbose,
					schemaSources: strings.TrimSpace(*bSchemaSources),
				})
				if err != nil {
					return err
				}
				if len(issues) > 0 {
					fmt.Println(formatIssuesText(issues))
				}
				if hadError || (*bFailOnWarn && hadWarn) {
					return fmt.Errorf("bundle check failed")
				}
			}
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
		var failures []error
		for _, filename := range files {
			if *pVerbose {
				fmt.Printf("Parsing %s...\n", filename)
			}

			file, err := comp.ParseFile(filename)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", filename, err)
				failures = append(failures, fmt.Errorf("parse %s: %w", filename, err))
				continue
			}

			fmt.Printf("Successfully parsed %s: %d rules, %d flows\n",
				filename, len(file.Rules), len(file.Flows))
		}

		return errors.Join(failures...)
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
		var failures []error
		for _, filename := range files {
			if *capVerbose {
				fmt.Printf("Analyzing capabilities in %s...\n", filename)
			}

			// Parse the file
			comp := compiler.NewCompiler()
			file, err := comp.ParseFile(filename)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing file %s: %v\n", filename, err)
				failures = append(failures, fmt.Errorf("parse %s: %w", filename, err))
				continue
			}

			// Analyze capabilities
			result, err := analyzer.Analyze(file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error analyzing file %s: %v\n", filename, err)
				failures = append(failures, fmt.Errorf("analyze %s: %w", filename, err))
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

		return errors.Join(failures...)
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
	commands["facts"] = newFactsCommand()
	commands["format"] = newFormatCommand()
	commands["resolve"] = newResolveCommand()
	commands["migrate-workflows"] = newMigrateWorkflowsCommand()
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
func createEmptyFacts(schemaFiles string, verbose bool) (*testFacts, *types.TypeSystem, error) {
	// Create a new type system
	typeSystem := types.NewTypeSystem()

	var failures []error
	// Load schema files if provided
	if schemaFiles != "" {
		files := expandSchemaPaths(strings.Split(schemaFiles, ","))
		for _, file := range files {
			if verbose {
				fmt.Printf("Loading schema from %s...\n", file)
			}
			if err := typeSystem.LoadSchemaFile(file); err != nil {
				if jsonErr := typeSystem.LoadJSONSchemaFile(file); jsonErr != nil {
					failures = append(failures, fmt.Errorf("load schema %s: %w", file, errors.Join(err, jsonErr)))
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

	return &testFacts{factRegistry: registry, schema: schemaInfo}, typeSystem, errors.Join(failures...)
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

func collectRuleFiles(dir string) ([]string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	ruleFiles := make([]string, 0)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext == ".eff" || ext == ".effx" {
			ruleFiles = append(ruleFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(ruleFiles)
	return ruleFiles, nil
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

// Type returns the registered type of a fact.
func (f *testFacts) Type(path string) interface{} {
	if f == nil || f.schema == nil || f.schema.typeSystem == nil {
		return nil
	}
	typ, err := f.schema.typeSystem.GetFactType(path)
	if err != nil {
		return nil
	}
	return typ
}

type legacyWorkflowDefinition struct {
	Name        string                         `json:"name"`
	Description string                         `json:"description,omitempty"`
	Priority    int32                          `json:"priority,omitempty"`
	Facts       map[string]string              `json:"facts,omitempty"`
	ErrorPolicy string                         `json:"errorPolicy,omitempty"`
	Parallel    bool                           `json:"parallel,omitempty"`
	Steps       []legacyWorkflowStepDefinition `json:"steps"`
}

type legacyWorkflowStepDefinition struct {
	ID        string                         `json:"id"`
	Verb      string                         `json:"verb"`
	Arguments map[string]legacyWorkflowValue `json:"arguments,omitempty"`
	Result    string                         `json:"result,omitempty"`
}

type legacyWorkflowValue struct {
	Literal  json.RawMessage `json:"literal,omitempty"`
	FactPath string          `json:"factPath,omitempty"`
	Result   string          `json:"result,omitempty"`
}

func newMigrateWorkflowsCommand() *Command {
	command := &Command{
		Name:        "migrate-workflows",
		Description: "Convert legacy JSON workflows to .effx source",
		FlagSet:     flag.NewFlagSet("migrate-workflows", flag.ContinueOnError),
	}
	output := command.FlagSet.String("output", "", "Output .effx file (defaults to stdout)")
	command.Run = func() error {
		arguments := command.FlagSet.Args()
		if len(arguments) != 1 {
			return fmt.Errorf("migrate-workflows requires exactly one legacy workflow manifest")
		}
		data, err := os.ReadFile(arguments[0])
		if err != nil {
			return err
		}
		workflows, err := decodeLegacyWorkflows(data)
		if err != nil {
			return err
		}
		if len(workflows) == 0 {
			return fmt.Errorf("manifest contains no workflows")
		}
		converted, err := renderLegacyWorkflows(workflows)
		if err != nil {
			return err
		}
		if *output == "" {
			fmt.Print(converted)
			return nil
		}
		if filepath.Ext(*output) != ".effx" {
			return fmt.Errorf("migration output must use .effx")
		}
		return os.WriteFile(*output, []byte(converted), 0o600)
	}
	return command
}

func decodeLegacyWorkflows(data []byte) ([]legacyWorkflowDefinition, error) {
	if err := rejectDuplicateMigrationJSON(data); err != nil {
		return nil, fmt.Errorf("decode legacy workflow manifest: %w", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode legacy workflow manifest: %w", err)
	}
	raw, ok := envelope["workflows"]
	if !ok {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var workflows []legacyWorkflowDefinition
	if err := decoder.Decode(&workflows); err != nil {
		return nil, fmt.Errorf("decode legacy workflows: %w", err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode legacy workflows: multiple JSON values")
		}
		return nil, fmt.Errorf("decode legacy workflows: %w", err)
	}
	return workflows, nil
}

func rejectDuplicateMigrationJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var scan func() error
	scan = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("JSON object key is not a string")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate JSON object key %q", key)
				}
				seen[key] = struct{}{}
				if err := scan(); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := scan(); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
		}
	}
	if err := scan(); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func renderLegacyWorkflows(workflows []legacyWorkflowDefinition) (string, error) {
	var output strings.Builder
	seen := make(map[string]struct{}, len(workflows))
	for index, workflow := range workflows {
		if strings.TrimSpace(workflow.Name) == "" || workflow.Name != strings.TrimSpace(workflow.Name) {
			return "", fmt.Errorf("workflow %d has an invalid name", index+1)
		}
		if _, duplicate := seen[workflow.Name]; duplicate {
			return "", fmt.Errorf("workflow %q is ambiguous because its name is duplicated", workflow.Name)
		}
		seen[workflow.Name] = struct{}{}
		if workflow.Parallel {
			return "", fmt.Errorf("workflow %q uses unsupported parallel execution", workflow.Name)
		}
		if workflow.ErrorPolicy != "" && workflow.ErrorPolicy != "fail" {
			return "", fmt.Errorf("workflow %q uses unsupported error policy %q", workflow.Name, workflow.ErrorPolicy)
		}
		if len(workflow.Facts) != 0 {
			return "", fmt.Errorf("workflow %q embeds fact declarations; move them to a schema manifest before migration", workflow.Name)
		}
		fmt.Fprintf(&output, "flow %s priority %d {\n  when {}\n  steps {\n", strconv.Quote(workflow.Name), workflow.Priority)
		bindings := make(map[string]struct{})
		for stepIndex, step := range workflow.Steps {
			if strings.TrimSpace(step.Verb) == "" || step.Verb != strings.TrimSpace(step.Verb) {
				return "", fmt.Errorf("workflow %q step %d has an invalid verb", workflow.Name, stepIndex+1)
			}
			if step.Result != "" {
				if strings.TrimSpace(step.Result) != step.Result {
					return "", fmt.Errorf("workflow %q step %d has an invalid result binding", workflow.Name, stepIndex+1)
				}
				if _, duplicate := bindings[step.Result]; duplicate {
					return "", fmt.Errorf("workflow %q repeats result binding %q", workflow.Name, step.Result)
				}
				bindings[step.Result] = struct{}{}
				fmt.Fprintf(&output, "    %s = ", step.Result)
			} else {
				output.WriteString("    ")
			}
			output.WriteString(step.Verb)
			output.WriteByte('(')
			names := make([]string, 0, len(step.Arguments))
			for name := range step.Arguments {
				names = append(names, name)
			}
			sort.Strings(names)
			for argumentIndex, name := range names {
				if argumentIndex > 0 {
					output.WriteString(", ")
				}
				value, err := renderLegacyWorkflowValue(step.Arguments[name], bindings)
				if err != nil {
					return "", fmt.Errorf("workflow %q step %d argument %q: %w", workflow.Name, stepIndex+1, name, err)
				}
				fmt.Fprintf(&output, "%s: %s", name, value)
			}
			output.WriteString(")\n")
		}
		output.WriteString("  }\n}\n")
		if index+1 < len(workflows) {
			output.WriteByte('\n')
		}
	}
	return output.String(), nil
}

func renderLegacyWorkflowValue(value legacyWorkflowValue, bindings map[string]struct{}) (string, error) {
	kinds := 0
	if value.Literal != nil {
		kinds++
	}
	if value.FactPath != "" {
		kinds++
	}
	if value.Result != "" {
		kinds++
	}
	if kinds != 1 {
		return "", fmt.Errorf("value must contain exactly one literal, factPath, or result")
	}
	if value.FactPath != "" {
		return value.FactPath, nil
	}
	if value.Result != "" {
		if _, ok := bindings[value.Result]; !ok {
			return "", fmt.Errorf("result binding %q is not available", value.Result)
		}
		return "$" + value.Result, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(value.Literal))
	decoder.UseNumber()
	var decoded interface{}
	if err := decoder.Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode literal: %w", err)
	}
	return renderEffectusLiteral(decoded)
}

func renderEffectusLiteral(value interface{}) (string, error) {
	switch value := value.(type) {
	case nil:
		return "", fmt.Errorf("null literals are not representable in .effx")
	case bool:
		return strconv.FormatBool(value), nil
	case string:
		return strconv.Quote(value), nil
	case json.Number:
		if _, err := strconv.ParseInt(string(value), 10, 64); err == nil {
			return string(value), nil
		}
		if _, err := strconv.ParseFloat(string(value), 64); err == nil {
			return string(value), nil
		}
		return "", fmt.Errorf("invalid number %q", value)
	case []interface{}:
		items := make([]string, len(value))
		for index, item := range value {
			rendered, err := renderEffectusLiteral(item)
			if err != nil {
				return "", err
			}
			items[index] = rendered
		}
		return "[" + strings.Join(items, " ") + "]", nil
	case map[string]interface{}:
		names := make([]string, 0, len(value))
		for name := range value {
			names = append(names, name)
		}
		sort.Strings(names)
		fields := make([]string, 0, len(names))
		for _, name := range names {
			rendered, err := renderEffectusLiteral(value[name])
			if err != nil {
				return "", err
			}
			fields = append(fields, name+": "+rendered)
		}
		return "{" + strings.Join(fields, " ") + "}", nil
	default:
		return "", fmt.Errorf("unsupported literal type %T", value)
	}
}
