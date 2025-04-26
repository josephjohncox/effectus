package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/schema"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "effectusc",
		Short: "Effectus rule compiler",
		Long:  "Effectus rule compiler - compiles .eff and .effx rule files",
	}

	rootCmd.AddCommand(
		newListCommand(),
		newFlowCommand(),
		newLintCommand(),
		newRunCommand(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func newListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [file]",
		Short: "Compile a list-style rule file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get rule file path
			rulePath := args[0]

			// Check file extension
			ext := filepath.Ext(rulePath)
			if ext != ".eff" {
				return fmt.Errorf("list command can only compile .eff files, got: %s", rulePath)
			}

			// Load schema (simplified - in a real implementation we'd load protobuf descriptors)
			schemaInfo := &schema.SimpleSchema{}

			// Create and run the compiler
			compiler := &list.Compiler{}
			spec, err := compiler.CompileFile(rulePath, schemaInfo)
			if err != nil {
				return fmt.Errorf("compilation failed: %w", err)
			}

			// Output compiled spec (simplified - would output JSON in a real implementation)
			fmt.Printf("Successfully compiled: %s\n", rulePath)
			fmt.Printf("Required facts: %v\n", spec.RequiredFacts())

			return nil
		},
	}

	return cmd
}

func newFlowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flow [file]",
		Short: "Compile a flow-style rule file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get rule file path
			rulePath := args[0]

			// Check file extension
			ext := filepath.Ext(rulePath)
			if ext != ".effx" {
				return fmt.Errorf("flow command can only compile .effx files, got: %s", rulePath)
			}

			// Load schema (simplified - in a real implementation we'd load protobuf descriptors)
			schemaInfo := &schema.SimpleSchema{}

			// Create and run the compiler
			compiler := &flow.Compiler{}
			spec, err := compiler.CompileFile(rulePath, schemaInfo)
			if err != nil {
				return fmt.Errorf("compilation failed: %w", err)
			}

			// Output compiled spec (simplified - would output JSON in a real implementation)
			fmt.Printf("Successfully compiled: %s\n", rulePath)
			fmt.Printf("Required facts: %v\n", spec.RequiredFacts())

			return nil
		},
	}

	return cmd
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
			if mode != "list" && mode != "flow" {
				return fmt.Errorf("invalid mode: %s, must be 'list' or 'flow'", mode)
			}

			// Simplified implementation
			fmt.Printf("Running in %s mode is not implemented yet\n", mode)
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "Execution mode (list|flow)")
	cmd.MarkFlagRequired("mode")

	return cmd
}
