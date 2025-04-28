package compiler

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/schema"
)

// Compiler handles parsing and type checking of Effectus files
type Compiler struct {
	parser      *participle.Parser[ast.File]
	typeChecker *schema.TypeChecker
}

// NewCompiler creates a new compiler
func NewCompiler() *Compiler {
	return &Compiler{
		parser:      createParser(),
		typeChecker: schema.NewTypeChecker(),
	}
}

// createParser creates the participle parser for the Effectus language
func createParser() *participle.Parser[ast.File] {
	// Define the lexer for the Effectus language
	effectusLexer := lexer.MustSimple([]lexer.SimpleRule{
		{"Comment", `//.*|/\*.*?\*/`},
		{"String", `"[^"]*"`},
		{"Float", `\d+\.\d+`},
		{"Int", `\d+`},
		{"FactPath", `[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)+`},
		{"VarRef", `\$[a-zA-Z_][a-zA-Z0-9_]*`},
		{"Ident", `[a-zA-Z_][a-zA-Z0-9_]*`},
		{"Operator", `==|!=|<=|>=|<|>|in|contains`},
		{"Punct", `[-[!@#$%^&*()+_={}\|:;"'<,>.?/]|]`},
		{"Whitespace", `[ \t\n\r]+`},
	})

	// Build the parser
	parser, err := participle.Build[ast.File](
		participle.Lexer(effectusLexer),
		participle.Elide("Comment", "Whitespace"),
	)
	if err != nil {
		// This should never happen unless there's a bug in the AST definitions
		panic(fmt.Sprintf("failed to create parser: %v", err))
	}
	return parser
}

// ParseFile parses a file into an AST
func (c *Compiler) ParseFile(filename string) (*ast.File, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	file, err := c.parser.ParseBytes(filename, data)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return file, nil
}

// ParseAndTypeCheck parses a file and performs type checking
func (c *Compiler) ParseAndTypeCheck(filename string, facts effectus.Facts) (*ast.File, error) {
	// Parse the file first
	file, err := c.ParseFile(filename)
	if err != nil {
		return nil, err
	}

	// Make sure we have registered default verb types
	if err := c.registerDefaultVerbTypes(); err != nil {
		return nil, fmt.Errorf("failed to register verb types: %w", err)
	}

	// Perform type checking
	if err := c.typeChecker.TypeCheckFile(file, facts); err != nil {
		return nil, fmt.Errorf("type check error: %w", err)
	}

	return file, nil
}

// registerDefaultVerbTypes registers some basic verb types for demonstration purposes
func (c *Compiler) registerDefaultVerbTypes() error {
	// SendEmail verb
	c.typeChecker.RegisterVerbSpec("SendEmail",
		map[string]*schema.Type{
			"to":      {PrimType: schema.TypeString},
			"subject": {PrimType: schema.TypeString},
			"body":    {PrimType: schema.TypeString},
		},
		&schema.Type{PrimType: schema.TypeBool})

	// LogOrder verb
	c.typeChecker.RegisterVerbSpec("LogOrder",
		map[string]*schema.Type{
			"order_id": {PrimType: schema.TypeString},
			"total":    {PrimType: schema.TypeFloat},
		},
		&schema.Type{PrimType: schema.TypeBool})

	// CreateOrder verb
	c.typeChecker.RegisterVerbSpec("CreateOrder",
		map[string]*schema.Type{
			"customer_id": {PrimType: schema.TypeString},
			"items": {
				PrimType: schema.TypeList,
				ListType: &schema.Type{Name: "OrderItem", PrimType: schema.TypeUnknown},
			},
		},
		&schema.Type{Name: "Order", PrimType: schema.TypeUnknown})

	return nil
}

// RegisterProtoTypes registers types from protobuf files
func (c *Compiler) RegisterProtoTypes(protoFile string) error {
	return c.typeChecker.RegisterProtoTypes(protoFile)
}

// GenerateTypeReport generates a human-readable report of inferred types
func (c *Compiler) GenerateTypeReport() string {
	return c.typeChecker.GenerateTypeReport()
}
