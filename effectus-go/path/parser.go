package path

import (
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// PathAST represents the parsed AST of a path
type PathAST struct {
	Namespace string        `parser:"@Ident"`
	Elements  []*ElementAST `parser:"( '.' @@ )*"`
}

// ElementAST represents a single element in the path AST
type ElementAST struct {
	Name      string        `parser:"@Ident"`
	Subscript *SubscriptAST `parser:"( '[' @@ ']' )?"`
}

// SubscriptAST represents a subscript (array index or map key)
type SubscriptAST struct {
	Index     *int    `parser:"@Int"`
	StringKey *string `parser:"| @String"`
}

// Define the lexer
var pathDefinition = lexer.MustStateful(lexer.Rules{
	"Root": {
		{Name: "whitespace", Pattern: `\s+`, Action: nil},
		{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_]*`, Action: nil},
		{Name: "Int", Pattern: `[0-9]+`, Action: nil},
		{Name: "Punct", Pattern: `[.\[\]]`, Action: nil},
		{Name: "String", Pattern: `"[^"]*"`, Action: nil},
	},
})

// Create the parser
var parser = participle.MustBuild[PathAST](
	participle.Lexer(pathDefinition),
	participle.Elide("whitespace"),
)

// ParsePath parses a string path into a namespace and path elements
func ParsePath(pathStr string) (string, []PathElement, error) {
	if pathStr == "" {
		return "", nil, fmt.Errorf("empty path")
	}

	// Parse the path using participle
	ast, err := parser.ParseString("", pathStr)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse path '%s': %w", pathStr, err)
	}

	// Convert AST to path elements
	elements := make([]PathElement, len(ast.Elements))
	for i, elem := range ast.Elements {
		pathElem := NewElement(elem.Name)

		if elem.Subscript != nil {
			if elem.Subscript.Index != nil {
				pathElem = pathElem.WithIndex(*elem.Subscript.Index)
			} else if elem.Subscript.StringKey != nil {
				// Remove surrounding quotes
				key := *elem.Subscript.StringKey
				key = strings.Trim(key, "\"")
				pathElem = pathElem.WithStringKey(key)
			}
		}

		elements[i] = pathElem
	}

	return ast.Namespace, elements, nil
}

// For debugging
func DebugParse(pathStr string) (*PathAST, error) {
	return parser.ParseString("", pathStr)
}

// ParseString parses a path string into a Path structure
func ParseString(pathStr string) (Path, error) {
	namespace, elements, err := ParsePath(pathStr)
	if err != nil {
		return Path{}, err
	}

	return Path{
		Namespace: namespace,
		Elements:  elements,
	}, nil
}

// ValidatePath checks if a path string is valid
func ValidatePath(pathStr string) bool {
	_, err := ParseString(pathStr)
	return err == nil
}
