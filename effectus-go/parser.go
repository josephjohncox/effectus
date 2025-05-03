package effectus

import (
	"fmt"
	"os"
	"path/filepath"

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
		// Add RawExpr for capturing expressions within when blocks
		{"VarRef", `\$[a-zA-Z_]\w*`}, // Variable references like $result
		// Enhanced FactPath pattern that supports:
		// - Regular paths (customer.email)
		// - Array indexing (customer.orders[0].id)
		// - Map keys (customer.attributes["name"])
		// - Mixed access (inventory.items[0].tags["color"])
		{"FactPath", `[a-zA-Z_]\w*(\.[a-zA-Z_]\w*|\.\d+|\[[0-9]+\]|\["[^"]*"\])+`},
		{"Operator", `==|!=|<=|>=|<|>|\bin\b|\bcontains\b`},
		{"LogicalOp", `&&|\|\|`},
		// Add tokens for common operators in expressions
		{"Dollar", `\$`},
		{"Ident", `[a-zA-Z_]\w*`},
		{"Arrow", `->`},
		{"Braces", `[{}]`},
		// {"RawString", "`[^`]+`"},
		// {"MultiLineBlock", `{[^\{\}]*}`},
		// {"RawExpr", `[a-zA-Z_]`},
		{"Punct", `[-[!@#%^&*()+_={}\|:;"'<,>.?/]|]`},
	})

	// parser is our participle parser for Effectus rule files
	parser = participle.MustBuild[ast.File](
		participle.Lexer(effectusLexer),
		// participle.Unquote("String"),
		participle.Elide("Comment", "Whitespace"),
		participle.UseLookahead(3),
	)
)

// GetParser returns the effectus parser for use by other packages
func GetParser() *participle.Parser[ast.File] {
	return parser
}

// GetLexer returns the effectus lexer for use by other packages
func GetLexer() lexer.Definition {
	return effectusLexer
}

// ParseFile parses a rule file and returns the AST
func ParseFile(filename string) (*ast.File, error) {
	// Read the file
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var file *ast.File

	// Parse directly with participle
	parsedFile, err := parser.ParseBytes(filename, content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}
	file = parsedFile

	// Process predicate blocks to extract expressions
	postProcessPredicates(file)

	fmt.Printf("Parsed successfully. Rules: %d, Flows: %d\n",
		len(file.Rules), len(file.Flows))

	// Validate file extension and content match
	err = validateFileType(filename, file)
	if err != nil {
		return nil, err
	}

	return file, nil
}

// postProcessPredicates processes all predicate blocks in the AST
func postProcessPredicates(file *ast.File) {
	// Process rules
	for _, rule := range file.Rules {
		for _, block := range rule.Blocks {
			if block.When != nil {
				block.When.PostProcess()
			}
		}
	}

	// Process flows
	for _, flow := range file.Flows {
		if flow.When != nil {
			flow.When.PostProcess()
		}
	}
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
