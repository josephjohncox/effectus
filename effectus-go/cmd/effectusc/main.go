package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/unified"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "effectusc",
		Short: "Effectus rule compiler",
		Long:  "Effectus rule compiler - compiles .eff and .effx rule files",
	}

	rootCmd.AddCommand(
		newCompileCommand(),
		newLintCommand(),
		newRunCommand(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func newCompileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compile [files]",
		Short: "Compile rule files (.eff and/or .effx)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get rule file paths
			rulePaths := args

			// Group files by type
			effFiles := []string{}
			effxFiles := []string{}

			for _, path := range rulePaths {
				ext := filepath.Ext(path)
				switch ext {
				case ".eff":
					effFiles = append(effFiles, path)
				case ".effx":
					effxFiles = append(effxFiles, path)
				default:
					return fmt.Errorf("unsupported file extension: %s (must be .eff or .effx)", ext)
				}
			}

			// Load schema (simplified - in a real implementation we'd load protobuf descriptors)
			schemaInfo := &schema.SimpleSchema{}

			// Create merged spec
			mergedSpec, err := compileAllFiles(effFiles, effxFiles, schemaInfo)
			if err != nil {
				return fmt.Errorf("compilation failed: %w", err)
			}

			// Output compiled spec
			fmt.Printf("Successfully compiled %d files\n", len(rulePaths))
			fmt.Printf("Required facts: %v\n", mergedSpec.RequiredFacts())

			return nil
		},
	}

	return cmd
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
		var flowErr error
		spec, flowErr := flowCompiler.CompileFiles(effxFiles, schema)
		if flowErr != nil {
			return nil, flowErr
		}
		// Type assertion to convert from effectus.Spec to *flow.Spec
		if spec != nil {
			var ok bool
			flowSpec, ok = spec.(*flow.Spec)
			if !ok {
				return nil, fmt.Errorf("unexpected spec type from flow compiler")
			}
		}
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

func newLintCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint [file|dir]",
		Short: "Lint rule files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Simplified implementation
			fmt.Println("Linting is not implemented yet")
			return nil
		},
	}

	return cmd
}

func newRunCommand() *cobra.Command {
	var mode string

	cmd := &cobra.Command{
		Use:   "run [file|dir]",
		Short: "Run rules against input facts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check mode
			if mode != "list" && mode != "flow" && mode != "unified" {
				return fmt.Errorf("invalid mode: %s, must be 'list', 'flow', or 'unified'", mode)
			}

			// Simplified implementation
			fmt.Printf("Running in %s mode is not implemented yet\n", mode)
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "unified", "Execution mode (list|flow|unified)")

	return cmd
}
