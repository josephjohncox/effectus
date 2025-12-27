package ast

import (
	"strings"

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
	Steps    *StepBlock      `parser:"'steps' '{' @@? '}'"`
	End      string          `parser:"'}'"`
}

// PredicateBlock represents a block of predicates as a raw expression string
type PredicateBlock struct {
	Pos        lexer.Position
	Expression string `parser:"@(Ident | Float | Int | String | Operator | LogicalOp | Punct | VarRef | FactPath)+(?=@Braces)"`
}

// PostProcess processes the raw expression to create a clean expression string
func (p *PredicateBlock) PostProcess() {
	if p == nil {
		return
	}
	// Trim whitespace and normalize the raw expression
	p.Expression = strings.TrimSpace(p.Expression)
}

// GetExpression returns the expression string
func (p *PredicateBlock) GetExpression() string {
	if p == nil {
		return ""
	}
	return p.Expression
}

// PathExpression represents a path in the AST
type PathExpression struct {
	Path string `parser:"@(FactPath | Ident)"` // The path string
}

// GetFullPath returns the path string
func (p *PathExpression) GetFullPath() string {
	if p == nil {
		return ""
	}
	return p.Path
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
