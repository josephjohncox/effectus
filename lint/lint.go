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

	CodeDeadRule            = "dead-rule"
	CodeDeadFlow            = "dead-flow"
	CodeMissingInverse      = "missing-inverse"
	CodeMissingConcurrency  = "missing-concurrency"
	CodeMissingCapability   = "missing-capability"
	CodeOverbroadCapability = "overbroad-capability"
	CodeMissingResources    = "missing-resources"
	CodeUnsafeExpression    = "unsafe-expression"
	CodeDivisionByZero      = "division-by-zero"
	CodeEmptyFactPath       = "empty-fact-path"
	CodeUnusedBinding       = "unused-binding"
	CodeBindingNoReturn     = "binding-no-return"
)

// UnsafeMode controls how unsafe expression usage is reported.
type UnsafeMode string

const (
	UnsafeIgnore UnsafeMode = "ignore"
	UnsafeWarn   UnsafeMode = "warn"
	UnsafeError  UnsafeMode = "error"
)

// VerbMode controls how verb-related lint issues are reported.
type VerbMode string

const (
	VerbIgnore VerbMode = "ignore"
	VerbWarn   VerbMode = "warn"
	VerbError  VerbMode = "error"
)

// LintOptions configures lint behavior.
type LintOptions struct {
	UnsafeMode UnsafeMode
	VerbMode   VerbMode
}

// DefaultOptions returns the default lint options.
func DefaultOptions() LintOptions {
	return LintOptions{UnsafeMode: UnsafeWarn, VerbMode: VerbError}
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

// ParseVerbMode parses a string into VerbMode.
func ParseVerbMode(raw string) (VerbMode, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	switch trimmed {
	case "", "error", "err":
		return VerbError, nil
	case "warn", "warning":
		return VerbWarn, nil
	case "ignore", "off", "none":
		return VerbIgnore, nil
	default:
		return VerbError, fmt.Errorf("unknown verb mode: %s", raw)
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
	verbMode := normalizeVerbMode(options.VerbMode)
	verbSeverity, verbEnabled := verbSeverity(verbMode)

	for _, rule := range file.Rules {
		for _, block := range rule.Blocks {
			if block.When != nil {
				issues = append(issues, lintDeadExpression(path, block.When.Pos, rule.Name, "rule", block.When.Expression)...)
				issues = append(issues, lintUnsafeExpression(path, block.When.Pos, rule.Name, "rule", block.When.Expression, mode)...)
				issues = append(issues, lintDivisionByZero(path, block.When.Pos, rule.Name, "rule", block.When.Expression)...)
			}
			if block.Then != nil {
				for _, effect := range block.Then.Effects {
					issues = append(issues, lintEmptyArgPath(path, effect.Pos, effect.Args)...)
					if verbEnabled {
						issues = append(issues, lintMissingInverse(path, effect.Pos, effect.Verb, verbs, verbSeverity)...)
						issues = append(issues, lintMissingCapability(path, effect.Pos, effect.Verb, verbs, verbSeverity)...)
						issues = append(issues, lintMissingResources(path, effect.Pos, effect.Verb, verbs, verbSeverity)...)
						issues = append(issues, lintMissingConcurrency(path, effect.Pos, effect.Verb, verbs, verbSeverity)...)
						issues = append(issues, lintOverbroadCapability(path, effect.Pos, effect.Verb, verbs, verbSeverity)...)
					}
				}
			}
		}
	}

	for _, flow := range file.Flows {
		if flow.When != nil {
			issues = append(issues, lintDeadExpression(path, flow.When.Pos, flow.Name, "flow", flow.When.Expression)...)
			issues = append(issues, lintUnsafeExpression(path, flow.When.Pos, flow.Name, "flow", flow.When.Expression, mode)...)
			issues = append(issues, lintDivisionByZero(path, flow.When.Pos, flow.Name, "flow", flow.When.Expression)...)
		}
		if flow.Steps != nil {
			issues = append(issues, lintFlowBindings(path, flow, verbs, verbSeverity, verbEnabled)...)
			for _, step := range flow.Steps.Steps {
				issues = append(issues, lintEmptyArgPath(path, step.Pos, step.Args)...)
				if verbEnabled {
					issues = append(issues, lintMissingInverse(path, step.Pos, step.Verb, verbs, verbSeverity)...)
					issues = append(issues, lintMissingCapability(path, step.Pos, step.Verb, verbs, verbSeverity)...)
					issues = append(issues, lintMissingResources(path, step.Pos, step.Verb, verbs, verbSeverity)...)
					issues = append(issues, lintMissingConcurrency(path, step.Pos, step.Verb, verbs, verbSeverity)...)
					issues = append(issues, lintOverbroadCapability(path, step.Pos, step.Verb, verbs, verbSeverity)...)
				}
			}
		}
	}

	return issues
}

func lintFlowBindings(path string, flow *ast.Flow, verbs VerbLookup, verbSeverity string, verbEnabled bool) []Issue {
	if flow == nil || flow.Steps == nil {
		return nil
	}

	type bindingInfo struct {
		pos  lexer.Position
		verb string
	}

	bindings := make(map[string]bindingInfo)
	used := make(map[string]struct{})
	issues := make([]Issue, 0)

	for _, step := range flow.Steps.Steps {
		if step == nil {
			continue
		}

		if step.BindName != "" {
			bindings[step.BindName] = bindingInfo{pos: step.Pos, verb: step.Verb}

			if verbEnabled && verbs != nil {
				if spec, ok := verbs.GetVerb(step.Verb); ok && spec != nil {
					if strings.TrimSpace(spec.ReturnType) == "" {
						issues = append(issues, Issue{
							File:     path,
							Pos:      step.Pos,
							Severity: verbSeverity,
							Code:     CodeBindingNoReturn,
							Message:  fmt.Sprintf("binding %q uses verb %q which has no return type", step.BindName, step.Verb),
						})
					}
				}
			}
		}

		for _, arg := range step.Args {
			if arg == nil || arg.Value == nil || arg.Value.VarRef == "" {
				continue
			}
			name := strings.TrimPrefix(arg.Value.VarRef, "$")
			if name != "" {
				used[name] = struct{}{}
			}
		}
	}

	for name, info := range bindings {
		if _, ok := used[name]; ok {
			continue
		}
		issues = append(issues, Issue{
			File:     path,
			Pos:      info.pos,
			Severity: SeverityWarning,
			Code:     CodeUnusedBinding,
			Message:  fmt.Sprintf("binding %q is never used in flow %q", name, flow.Name),
		})
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

func lintMissingInverse(path string, pos lexer.Position, verbName string, verbs VerbLookup, severity string) []Issue {
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
			Severity: severity,
			Code:     CodeMissingInverse,
			Message:  fmt.Sprintf("verb %q mutates state but has no inverse defined", verbName),
		},
	}
}

func lintMissingCapability(path string, pos lexer.Position, verbName string, verbs VerbLookup, severity string) []Issue {
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
			Severity: severity,
			Code:     CodeMissingCapability,
			Message:  fmt.Sprintf("verb %q has no capability declared", verbName),
		},
	}
}

func lintMissingResources(path string, pos lexer.Position, verbName string, verbs VerbLookup, severity string) []Issue {
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
			Severity: severity,
			Code:     CodeMissingResources,
			Message:  fmt.Sprintf("verb %q mutates state but declares no resources", verbName),
		},
	}
}

func lintMissingConcurrency(path string, pos lexer.Position, verbName string, verbs VerbLookup, severity string) []Issue {
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

	if hasConcurrencyFlag(spec.Capability) {
		return nil
	}

	for _, resource := range spec.Resources {
		if hasConcurrencyFlag(resource.Cap) {
			return nil
		}
	}

	return []Issue{
		{
			File:     path,
			Pos:      pos,
			Severity: severity,
			Code:     CodeMissingConcurrency,
			Message:  fmt.Sprintf("verb %q lacks concurrency semantics; set idempotent, commutative, or exclusive capability", verbName),
		},
	}
}

func lintOverbroadCapability(path string, pos lexer.Position, verbName string, verbs VerbLookup, severity string) []Issue {
	if verbs == nil {
		return nil
	}

	spec, ok := verbs.GetVerb(verbName)
	if !ok || spec == nil {
		return nil
	}

	issues := make([]Issue, 0)

	if spec.Capability&verb.CapAll == verb.CapAll {
		issues = append(issues, Issue{
			File:     path,
			Pos:      pos,
			Severity: severity,
			Code:     CodeOverbroadCapability,
			Message:  fmt.Sprintf("verb %q declares broad capabilities; prefer minimal or resource-scoped caps", verbName),
		})
	}

	for _, resource := range spec.Resources {
		if resource.Cap&verb.CapAll == verb.CapAll {
			issues = append(issues, Issue{
				File:     path,
				Pos:      pos,
				Severity: severity,
				Code:     CodeOverbroadCapability,
				Message:  fmt.Sprintf("verb %q resource %q declares broad capabilities", verbName, resource.Resource),
			})
		}
	}

	return issues
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

func lintDivisionByZero(path string, pos lexer.Position, name, kind, expression string) []Issue {
	exprText := strings.TrimSpace(expression)
	if exprText == "" {
		return nil
	}

	if !hasDivisionByZero(exprText) {
		return nil
	}

	return []Issue{
		{
			File:     path,
			Pos:      pos,
			Severity: SeverityError,
			Code:     CodeDivisionByZero,
			Message:  fmt.Sprintf("%s %q divides by zero in expression", kind, name),
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

func normalizeVerbMode(mode VerbMode) VerbMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case string(VerbWarn):
		return VerbWarn
	case string(VerbIgnore):
		return VerbIgnore
	default:
		return VerbError
	}
}

func verbSeverity(mode VerbMode) (string, bool) {
	switch mode {
	case VerbIgnore:
		return "", false
	case VerbWarn:
		return SeverityWarning, true
	default:
		return SeverityError, true
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

func hasDivisionByZero(expression string) bool {
	tree, err := parser.Parse(expression)
	if err != nil {
		return false
	}

	found := false
	var visit func(node exprast.Node)
	visit = func(node exprast.Node) {
		if node == nil || found {
			return
		}

		switch n := node.(type) {
		case *exprast.BinaryNode:
			if n.Operator == "/" && isZeroLiteral(n.Right) {
				found = true
				return
			}
			visit(n.Left)
			visit(n.Right)
		case *exprast.CallNode:
			visit(n.Callee)
			for _, arg := range n.Arguments {
				visit(arg)
			}
		case *exprast.MemberNode:
			visit(n.Node)
			visit(n.Property)
		case *exprast.UnaryNode:
			visit(n.Node)
		case *exprast.ChainNode:
			visit(n.Node)
		case *exprast.SliceNode:
			visit(n.Node)
			if n.From != nil {
				visit(n.From)
			}
			if n.To != nil {
				visit(n.To)
			}
		case *exprast.BuiltinNode:
			for _, arg := range n.Arguments {
				visit(arg)
			}
		case *exprast.PredicateNode:
			visit(n.Node)
		case *exprast.SequenceNode:
			for _, child := range n.Nodes {
				visit(child)
			}
		case *exprast.ConditionalNode:
			visit(n.Cond)
			visit(n.Exp1)
			visit(n.Exp2)
		case *exprast.ArrayNode:
			for _, child := range n.Nodes {
				visit(child)
			}
		case *exprast.MapNode:
			for _, pair := range n.Pairs {
				visit(pair)
			}
		case *exprast.PairNode:
			visit(n.Key)
			visit(n.Value)
		}
	}

	visit(tree.Node)
	return found
}

func isZeroLiteral(node exprast.Node) bool {
	switch n := node.(type) {
	case *exprast.IntegerNode:
		return n.Value == 0
	case *exprast.FloatNode:
		return n.Value == 0
	case *exprast.UnaryNode:
		return n.Operator == "-" && isZeroLiteral(n.Node)
	default:
		return false
	}
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

func hasConcurrencyFlag(cap verb.Capability) bool {
	return cap&(verb.CapIdempotent|verb.CapCommutative|verb.CapExclusive) != 0
}

func lintEmptyArgPath(path string, pos lexer.Position, args []*ast.StepArg) []Issue {
	if len(args) == 0 {
		return nil
	}

	issues := make([]Issue, 0)
	for _, arg := range args {
		if arg == nil || arg.Value == nil || arg.Value.PathExpr == nil {
			continue
		}
		if strings.TrimSpace(arg.Value.PathExpr.Path) != "" {
			continue
		}
		issues = append(issues, Issue{
			File:     path,
			Pos:      pos,
			Severity: SeverityError,
			Code:     CodeEmptyFactPath,
			Message:  "empty fact path used in argument expression",
		})
	}
	return issues
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
