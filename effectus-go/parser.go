package effectus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"

	"github.com/effectus/effectus-go/ast"
)

var (
	// effectusLexer defines the tokens for our language
	effectusLexer = lexer.MustSimple([]lexer.SimpleRule{
		{"Comment", `//.*|/\*.*?\*/`},
		{"Whitespace", `\s+`},
		{"Float", `[-+]?\d*\.\d+([eE][-+]?\d+)?`},
		{"Int", `[-+]?\d+`},
		{"String", `"[^"]*"`},
		{"VarRef", `\$[a-zA-Z_]\w*`},                 // Variable references like $result
		{"FactPath", `[a-zA-Z_]\w*\.[a-zA-Z_0-9.]+`}, // Fact paths like customer.email
		{"Arrow", `->`},                              // Binding operator
		{"Operator", `==|!=|<=|>=|<|>|in|contains`},
		{"Keyword", `\b(rule|flow|when|then|steps|include|priority|true|false)\b`},
		{"Ident", `[a-zA-Z_]\w*`},
		{"Punct", `[-[!@#%^&*()+_={}\|:;"'<,>.?/]|]`},
	})

	// parser is our participle parser for Effectus rule files
	parser = participle.MustBuild[ast.File](
		participle.Lexer(effectusLexer),
		participle.Elide("Comment", "Whitespace"),
		participle.UseLookahead(3),
	)
)

// ParseFile parses a rule file and returns the AST
func ParseFile(filename string) (*ast.File, error) {
	// Read the file
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Debug: Print first few characters of the file
	fmt.Printf("Parsing file: %s\n", filename)
	if len(content) > 20 {
		fmt.Printf("First 20 chars: %q\n", content[:20])
	} else {
		fmt.Printf("Content: %q\n", content)
	}

	// Parse with participle
	file, err := parser.ParseBytes(filename, content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	fmt.Printf("Parsed successfully. Includes: %d, Rules: %d, Flows: %d\n",
		len(file.Includes), len(file.Rules), len(file.Flows))

	// Validate file extension and content match
	err = validateFileType(filename, file)
	if err != nil {
		return nil, err
	}

	// Process includes
	err = processIncludes(file, filepath.Dir(filename))
	if err != nil {
		return nil, fmt.Errorf("failed to process includes: %w", err)
	}

	return file, nil
}

// processIncludes processes include directives in the parsed file
func processIncludes(file *ast.File, basePath string) error {
	if len(file.Includes) == 0 {
		return nil
	}

	// Process each include directive
	var processedRules []*ast.Rule
	var processedFlows []*ast.Flow

	// First add all the includes
	for _, include := range file.Includes {
		// Extract the path from the include statement (remove quotes)
		includePath := strings.Trim(include.Path, "\"")

		// Resolve the full path
		fullPath := filepath.Join(basePath, includePath)

		// Validate include file extension
		if err := validateIncludePath(includePath, fullPath); err != nil {
			return err
		}

		// Parse the included file
		includedFile, err := ParseFile(fullPath)
		if err != nil {
			return fmt.Errorf("failed to parse include file %s: %w", includePath, err)
		}

		// Check if we need to convert rules to flows (.eff in .effx)
		sourceExt := filepath.Ext(includePath)
		targetExt := filepath.Ext(filepath.Base(basePath))

		if sourceExt == ".eff" && targetExt == ".effx" {
			// Convert rules to flows
			convertedFlows := convertRulesToFlows(includedFile.Rules)
			processedFlows = append(processedFlows, convertedFlows...)
		} else {
			// Collect rules and flows from included file normally
			processedRules = append(processedRules, includedFile.Rules...)
			processedFlows = append(processedFlows, includedFile.Flows...)
		}
	}

	// Then add original file's content
	processedRules = append(processedRules, file.Rules...)
	processedFlows = append(processedFlows, file.Flows...)

	// Replace the original rules and flows with processed ones
	file.Rules = processedRules
	file.Flows = processedFlows

	// Clear includes since they've been processed
	file.Includes = nil

	return nil
}

// convertRulesToFlows converts list-style rules to flow-style rules
func convertRulesToFlows(rules []*ast.Rule) []*ast.Flow {
	flows := make([]*ast.Flow, 0, len(rules))

	for _, rule := range rules {
		// Create step block from effect block
		stepBlock := &ast.StepBlock{
			Steps: make([]*ast.Step, 0, len(rule.Then.Effects)),
		}

		// Convert each effect to a step (without binding)
		for _, effect := range rule.Then.Effects {
			// Convert the effect args to StepArgs
			args := make([]*ast.StepArg, 0, len(effect.Args))

			for i, arg := range effect.Args {
				// Create a default parameter name based on position
				paramName := fmt.Sprintf("param%d", i+1)

				// Create a step arg with the literal value
				args = append(args, &ast.StepArg{
					Name: paramName,
					Value: &ast.ArgValue{
						Literal: &arg,
					},
				})
			}

			stepBlock.Steps = append(stepBlock.Steps, &ast.Step{
				Verb: effect.Verb,
				Args: args,
				// No BindName for converted effects
			})
		}

		// Create the flow from the rule
		flow := &ast.Flow{
			Name:     rule.Name,
			Priority: rule.Priority,
			When:     rule.When,
			Steps:    stepBlock,
		}

		flows = append(flows, flow)
	}

	return flows
}

// validateFileType ensures the file extension matches its content
func validateFileType(filename string, file *ast.File) error {
	ext := filepath.Ext(filename)

	// Check for rules in .eff files
	if ext == ".eff" && len(file.Flows) > 0 {
		return fmt.Errorf("file %s has .eff extension but contains flow definitions", filename)
	}

	// Check for flows in .effx files
	if ext == ".effx" && len(file.Rules) > 0 {
		return fmt.Errorf("file %s has .effx extension but contains rule definitions", filename)
	}

	return nil
}

// validateIncludePath ensures includes reference the correct file type
func validateIncludePath(includePath string, fullPath string) error {
	sourceExt := filepath.Ext(fullPath)

	// Check that .eff only includes .eff or .effx files
	if filepath.Ext(includePath) == ".eff" && sourceExt != ".eff" && sourceExt != ".effx" {
		return fmt.Errorf("invalid include: .eff file can only include .eff or .effx files, got: %s", fullPath)
	}

	// Allow .effx to include both .effx and .eff files
	// .eff files will be transformed into steps with no output
	if filepath.Ext(includePath) == ".effx" && sourceExt != ".effx" && sourceExt != ".eff" {
		return fmt.Errorf("invalid include: .effx file can only include .effx or .eff files, got: %s", fullPath)
	}

	return nil
}

// Helper functions for creating pointers (useful for tests)
func strptr(s string) *string {
	return &s
}

func intptr(i int) *int {
	return &i
}

func floatptr(f float64) *float64 {
	return &f
}
