package lint

import (
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/expr-lang/expr"
	exprast "github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

const (
	SeverityWarning = "warning"
	SeverityError   = "error"

	CodeDeadRule          = "dead-rule"
	CodeDeadFlow          = "dead-flow"
	CodeMissingInverse    = "missing-inverse"
	CodeMissingCapability = "missing-capability"
	CodeMissingResources  = "missing-resources"
	CodeUnsafeExpression  = "unsafe-expression"
)

// UnsafeMode controls how unsafe expression usage is reported.
type UnsafeMode string

const (
	UnsafeIgnore UnsafeMode = "ignore"
	UnsafeWarn   UnsafeMode = "warn"
	UnsafeError  UnsafeMode = "error"
)

// LintOptions configures lint behavior.
type LintOptions struct {
	UnsafeMode UnsafeMode
}

// DefaultOptions returns the default lint options.
func DefaultOptions() LintOptions {
	return LintOptions{UnsafeMode: UnsafeWarn}
}

// ParseUnsafeMode parses a string into UnsafeMode.
func ParseUnsafeMode(raw string) (UnsafeMode, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	switch trimmed {
	case "", "warn", "warning":
		return UnsafeWarn, nil
	case "error", "err":
		return UnsafeError, nil
	case "ignore", "off", "none":
		return UnsafeIgnore, nil
	default:
		return UnsafeWarn, fmt.Errorf("unknown unsafe mode: %s", raw)
	}
}

// Issue represents a linter finding.
type Issue struct {
	File     string
	Pos      lexer.Position
	Severity string
	Code     string
	Message  string
}

// VerbLookup provides access to verb specifications.
type VerbLookup interface {
	GetVerb(name string) (*verb.Spec, bool)
}

// LintFile runs lint checks on a parsed file.
func LintFile(file *ast.File, path string, verbs VerbLookup) []Issue {
	return LintFileWithOptions(file, path, verbs, DefaultOptions())
}

// LintFileWithOptions runs lint checks with custom options.
func LintFileWithOptions(file *ast.File, path string, verbs VerbLookup, options LintOptions) []Issue {
	if file == nil {
		return nil
	}

	issues := make([]Issue, 0)
	mode := normalizeUnsafeMode(options.UnsafeMode)

	for _, rule := range file.Rules {
		for _, block := range rule.Blocks {
			if block.When != nil {
				issues = append(issues, lintDeadExpression(path, block.When.Pos, rule.Name, "rule", block.When.Expression)...)
				issues = append(issues, lintUnsafeExpression(path, block.When.Pos, rule.Name, "rule", block.When.Expression, mode)...)
			}
			if block.Then != nil {
				for _, effect := range block.Then.Effects {
					issues = append(issues, lintMissingInverse(path, effect.Pos, effect.Verb, verbs)...)
					issues = append(issues, lintMissingCapability(path, effect.Pos, effect.Verb, verbs)...)
					issues = append(issues, lintMissingResources(path, effect.Pos, effect.Verb, verbs)...)
				}
			}
		}
	}

	for _, flow := range file.Flows {
		if flow.When != nil {
			issues = append(issues, lintDeadExpression(path, flow.When.Pos, flow.Name, "flow", flow.When.Expression)...)
			issues = append(issues, lintUnsafeExpression(path, flow.When.Pos, flow.Name, "flow", flow.When.Expression, mode)...)
		}
		if flow.Steps != nil {
			for _, step := range flow.Steps.Steps {
				issues = append(issues, lintMissingInverse(path, step.Pos, step.Verb, verbs)...)
				issues = append(issues, lintMissingCapability(path, step.Pos, step.Verb, verbs)...)
				issues = append(issues, lintMissingResources(path, step.Pos, step.Verb, verbs)...)
			}
		}
	}

	return issues
}

func lintDeadExpression(path string, pos lexer.Position, name, kind, expression string) []Issue {
	exprText := strings.TrimSpace(expression)
	if exprText == "" {
		return nil
	}

	value, ok := constantBool(exprText)
	if !ok || value {
		return nil
	}

	return []Issue{
		{
			File:     path,
			Pos:      pos,
			Severity: SeverityWarning,
			Code:     deadCodeForKind(kind),
			Message:  fmt.Sprintf("%s %q can never match because the condition is always false", kind, name),
		},
	}
}

func deadCodeForKind(kind string) string {
	if strings.EqualFold(kind, "flow") {
		return CodeDeadFlow
	}
	return CodeDeadRule
}

func lintMissingInverse(path string, pos lexer.Position, verbName string, verbs VerbLookup) []Issue {
	if verbs == nil {
		return nil
	}

	spec, ok := verbs.GetVerb(verbName)
	if !ok || spec == nil {
		return nil
	}

	if spec.Inverse != "" {
		return nil
	}

	if !requiresInverse(spec) {
		return nil
	}

	return []Issue{
		{
			File:     path,
			Pos:      pos,
			Severity: SeverityWarning,
			Code:     CodeMissingInverse,
			Message:  fmt.Sprintf("verb %q mutates state but has no inverse defined", verbName),
		},
	}
}

func lintMissingCapability(path string, pos lexer.Position, verbName string, verbs VerbLookup) []Issue {
	if verbs == nil {
		return nil
	}

	spec, ok := verbs.GetVerb(verbName)
	if !ok || spec == nil {
		return nil
	}

	if spec.Capability != verb.CapNone && spec.Capability != verb.CapDefault {
		return nil
	}

	return []Issue{
		{
			File:     path,
			Pos:      pos,
			Severity: SeverityWarning,
			Code:     CodeMissingCapability,
			Message:  fmt.Sprintf("verb %q has no capability declared", verbName),
		},
	}
}

func lintMissingResources(path string, pos lexer.Position, verbName string, verbs VerbLookup) []Issue {
	if verbs == nil {
		return nil
	}

	spec, ok := verbs.GetVerb(verbName)
	if !ok || spec == nil {
		return nil
	}

	if !requiresInverse(spec) {
		return nil
	}

	if len(spec.Resources) > 0 {
		return nil
	}

	return []Issue{
		{
			File:     path,
			Pos:      pos,
			Severity: SeverityWarning,
			Code:     CodeMissingResources,
			Message:  fmt.Sprintf("verb %q mutates state but declares no resources", verbName),
		},
	}
}

func lintUnsafeExpression(path string, pos lexer.Position, name, kind, expression string, mode UnsafeMode) []Issue {
	if mode == UnsafeIgnore {
		return nil
	}

	exprText := strings.TrimSpace(expression)
	if exprText == "" {
		return nil
	}

	unsafeFuncs := unsafeFunctionNames()
	usage := findUnsafeOps(exprText, unsafeFuncs)
	if len(usage) == 0 {
		return nil
	}

	severity := SeverityWarning
	if mode == UnsafeError {
		severity = SeverityError
	}

	message := fmt.Sprintf("%s %q uses unsafe expression operations: %s", kind, name, strings.Join(usage, ", "))
	return []Issue{
		{
			File:     path,
			Pos:      pos,
			Severity: severity,
			Code:     CodeUnsafeExpression,
			Message:  message,
		},
	}
}

func normalizeUnsafeMode(mode UnsafeMode) UnsafeMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case string(UnsafeError):
		return UnsafeError
	case string(UnsafeIgnore):
		return UnsafeIgnore
	default:
		return UnsafeWarn
	}
}

func unsafeFunctionNames() map[string]struct{} {
	funcs := make(map[string]struct{})
	for _, spec := range types.StandardLibrary() {
		if spec != nil && spec.Unsafe {
			funcs[spec.Name] = struct{}{}
		}
	}
	return funcs
}

func findUnsafeOps(expression string, unsafeFuncs map[string]struct{}) []string {
	tree, err := parser.Parse(expression)
	if err != nil {
		return fallbackUnsafeOps(expression, unsafeFuncs)
	}

	seen := make(map[string]struct{})

	var visit func(node exprast.Node, parent exprast.Node)
	visit = func(node exprast.Node, parent exprast.Node) {
		if node == nil {
			return
		}

		switch n := node.(type) {
		case *exprast.BinaryNode:
			if strings.EqualFold(n.Operator, "matches") {
				seen["matches"] = struct{}{}
			}
			visit(n.Left, n)
			visit(n.Right, n)
		case *exprast.CallNode:
			if ident, ok := n.Callee.(*exprast.IdentifierNode); ok {
				if _, ok := unsafeFuncs[ident.Value]; ok {
					seen[ident.Value] = struct{}{}
				}
			}
			visit(n.Callee, n)
			for _, arg := range n.Arguments {
				visit(arg, n)
			}
		case *exprast.MemberNode:
			visit(n.Node, n)
			visit(n.Property, n)
		case *exprast.UnaryNode:
			visit(n.Node, n)
		case *exprast.ChainNode:
			visit(n.Node, n)
		case *exprast.SliceNode:
			visit(n.Node, n)
			if n.From != nil {
				visit(n.From, n)
			}
			if n.To != nil {
				visit(n.To, n)
			}
		case *exprast.BuiltinNode:
			for _, arg := range n.Arguments {
				visit(arg, n)
			}
		case *exprast.PredicateNode:
			visit(n.Node, n)
		case *exprast.SequenceNode:
			for _, child := range n.Nodes {
				visit(child, n)
			}
		case *exprast.ConditionalNode:
			visit(n.Cond, n)
			visit(n.Exp1, n)
			visit(n.Exp2, n)
		case *exprast.ArrayNode:
			for _, child := range n.Nodes {
				visit(child, n)
			}
		case *exprast.MapNode:
			for _, pair := range n.Pairs {
				visit(pair, n)
			}
		case *exprast.PairNode:
			visit(n.Key, n)
			visit(n.Value, n)
		}
	}

	visit(tree.Node, nil)

	for _, op := range fallbackUnsafeOps(expression, unsafeFuncs) {
		seen[op] = struct{}{}
	}

	results := make([]string, 0, len(seen))
	for op := range seen {
		results = append(results, op)
	}
	return results
}

func fallbackUnsafeOps(expression string, unsafeFuncs map[string]struct{}) []string {
	seen := make(map[string]struct{})
	lower := strings.ToLower(expression)
	if strings.Contains(lower, "matches") {
		seen["matches"] = struct{}{}
	}
	for name := range unsafeFuncs {
		if strings.Contains(lower, strings.ToLower(name)) {
			seen[name] = struct{}{}
		}
	}
	results := make([]string, 0, len(seen))
	for op := range seen {
		results = append(results, op)
	}
	return results
}

func requiresInverse(spec *verb.Spec) bool {
	if spec == nil {
		return false
	}

	mutating := spec.Capability&(verb.CapWrite|verb.CapCreate|verb.CapDelete) != 0
	if mutating {
		return true
	}

	for _, resource := range spec.Resources {
		if resource.Cap&(verb.CapWrite|verb.CapCreate|verb.CapDelete) != 0 {
			return true
		}
	}

	return false
}

func constantBool(expression string) (bool, bool) {
	tree, err := parser.Parse(expression)
	if err != nil {
		return false, false
	}

	visitor := &variableVisitor{}
	node := tree.Node
	exprast.Walk(&node, visitor)
	if visitor.hasVariables {
		return false, false
	}

	result, err := expr.Eval(expression, map[string]interface{}{})
	if err != nil {
		return false, false
	}
	value, ok := result.(bool)
	return value, ok
}

type variableVisitor struct {
	hasVariables bool
}

func (v *variableVisitor) Visit(node *exprast.Node) {
	switch (*node).(type) {
	case *exprast.IdentifierNode, *exprast.MemberNode, *exprast.PointerNode, *exprast.VariableDeclaratorNode:
		v.hasVariables = true
	}
}
