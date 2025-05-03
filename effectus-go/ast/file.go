package ast

import (
	"github.com/alecthomas/participle/v2/lexer"
)

// File represents a parsed rule file, containing either Rules or Flows
type File struct {
	Pos   lexer.Position
	Rules []*Rule `parser:"@@*"`
	Flows []*Flow `parser:"@@*"`
}

// Rule represents a list-style rule (when-then)
type Rule struct {
	Pos      lexer.Position
	Name     string           `parser:"'rule' @String"`
	Priority int              `parser:"'priority' @Int '{'"`
	Blocks   []*WhenThenBlock `parser:"@@*"`
	End      string           `parser:"'}'"`
}

// WhenThenBlock represents a when-then pair within a rule
type WhenThenBlock struct {
	Pos  lexer.Position
	When *PredicateBlock `parser:"'when' '{' @@? '}'"`
	Then *EffectBlock    `parser:"'then' '{' @@? '}'"`
}

// Flow represents a flow-style rule (when-steps)
type Flow struct {
	Pos      lexer.Position
	Name     string          `parser:"'flow' @String"`
	Priority int             `parser:"'priority' @Int '{'"`
	When     *PredicateBlock `parser:"'when' '{' @@? '}'"`
	Steps    *StepBlock      `parser:"'steps' '{' @@? '}' '}'"`
}

// PredicateBlock represents a block of predicates
type PredicateBlock struct {
	Pos        lexer.Position
	Expression *LogicalExpression `parser:"@@"`
}

// LogicalExpression represents a logical expression with AND/OR operators
type LogicalExpression struct {
	Pos   lexer.Position
	Left  *PredicateTerm     `parser:"@@"`
	Op    string             `parser:"(@LogicalOp"`
	Right *LogicalExpression `parser:"@@)?"`
}

// PredicateTerm represents either a predicate or a parenthesized logical expression
type PredicateTerm struct {
	Pos       lexer.Position
	Predicate *Predicate         `parser:"@@"`
	SubExpr   *LogicalExpression `parser:"| '(' @@ ')'"`
}

// Predicate represents a single condition
type Predicate struct {
	Pos      lexer.Position
	PathExpr *PathExpression `parser:"@@"`
	Op       string          `parser:"@Operator"`
	Lit      Literal         `parser:"@@"`
}

// PathExpression represents a parsed path in the AST
type PathExpression struct {
	Raw string `parser:"@(FactPath | Ident)"` // The raw path string

	// Resolved will be filled during post-processing
	Namespace string
	Segments  []string

	// For array indexing and more complex paths
	// The indexed path segments stores path segments with index information
	// (segment name and index, if any). This is filled during resolution.
	IndexedSegments []PathSegmentInfo
}

// PathSegmentInfo contains information about a segment in a path, including indexing
type PathSegmentInfo struct {
	Name  string // The segment name
	Index *int   // Optional array index (nil if not indexed)
}

// EffectBlock represents a block of effects
type EffectBlock struct {
	Pos     lexer.Position
	Effects []*Effect `parser:"@@*"`
}

// Effect represents a verb and its arguments
type Effect struct {
	Pos      lexer.Position
	BindName string     `parser:"(@Ident '=')?"` // Optional variable binding
	Verb     string     `parser:"@Ident"`
	Args     []*StepArg `parser:"'(' @@? (',' @@)* ')'"`
}

// StepBlock represents a block of steps
type StepBlock struct {
	Pos   lexer.Position
	Steps []*Step `parser:"@@*"`
}

// Step represents a single step in a flow
type Step struct {
	Pos      lexer.Position
	BindName string     `parser:"(@Ident '=')?"` // Optional variable binding
	Verb     string     `parser:"@Ident"`
	Args     []*StepArg `parser:"'(' @@? (',' @@)* ')'"`
	Arrow    string     `parser:"('->' @Ident)?"`
}

// StepArg represents an argument to a step or effect
type StepArg struct {
	Pos   lexer.Position
	Name  string    `parser:"@Ident ':'"`
	Value *ArgValue `parser:"@@"`
}

// ArgValue represents the value of an argument, which can be a literal,
// a variable reference, or a fact path
type ArgValue struct {
	Pos      lexer.Position
	VarRef   string          `parser:"  @VarRef"`
	PathExpr *PathExpression `parser:"| @@"`
	Literal  *Literal        `parser:"| @@"`
}

// Literal represents a literal value (string, number, boolean, etc.)
type Literal struct {
	Pos    lexer.Position
	String *string     `parser:"@String"`
	Int    *int        `parser:"| @Int"`
	Float  *float64    `parser:"| @Float"`
	Bool   *bool       `parser:"| @('true' | 'false')"`
	List   []Literal   `parser:"| '[' @@* ']'"`
	Map    []*MapEntry `parser:"| '{' @@* '}'"`
}

// MapEntry represents a key-value pair in a map
type MapEntry struct {
	Pos   lexer.Position
	Key   string  `parser:"@Ident ':'"`
	Value Literal `parser:"@@"`
}
