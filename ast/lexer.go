package ast

import "github.com/alecthomas/participle/v2/lexer"

// Lexer defines the token rules for parsing Effectus files.
var Lexer = lexer.MustStateful(lexer.Rules{
	"Root": {
		{Name: "Comment", Pattern: `//[^\n]*|#[^\n]*`, Action: nil},
		{Name: "Whitespace", Pattern: `\s+`, Action: nil},
		{Name: "Float", Pattern: `\d+\.\d+`, Action: nil},
		{Name: "Int", Pattern: `\d+`, Action: nil},
		{Name: "String", Pattern: `"(?:\\.|[^"])*"|'(?:\\.|[^'])*'`, Action: nil},
		{Name: "LogicalOp", Pattern: `&&|\|\||!`, Action: nil},
		{Name: "Operator", Pattern: `==|!=|>=|<=|>|<|\+|-|\*|/|%`, Action: nil},
		{Name: "VarRef", Pattern: `\$[a-zA-Z_][a-zA-Z0-9_]*`, Action: nil},
		{Name: "FactPath", Pattern: `[a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*|\[\d+\])+`, Action: nil},
		{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_]*`, Action: nil},
		{Name: "Punct", Pattern: `[(),\[\].:?]`, Action: nil},
		{Name: "Braces", Pattern: `[{}]`, Action: nil},
	},
})
