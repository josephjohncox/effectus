package schema

import (
	"fmt"
	"regexp"
	"strings"

	exprast "github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

var arrayIndexPattern = regexp.MustCompile(`\[[0-9]+\]`)

// ExtractFactPaths returns fact paths referenced by an expression.
func ExtractFactPaths(expression string) map[string]struct{} {
	paths := make(map[string]struct{})
	if strings.TrimSpace(expression) == "" {
		return paths
	}

	tree, err := parser.Parse(expression)
	if err != nil {
		return paths
	}

	var visit func(node exprast.Node, parent exprast.Node)
	visit = func(node exprast.Node, parent exprast.Node) {
		if node == nil {
			return
		}

		switch n := node.(type) {
		case *exprast.IdentifierNode:
			if _, isCall := parent.(*exprast.CallNode); isCall {
				return
			}
			if _, isMember := parent.(*exprast.MemberNode); isMember {
				return
			}
			paths[normalizeArrayPath(n.Value)] = struct{}{}
		case *exprast.MemberNode:
			if path, ok := memberPath(n); ok {
				paths[normalizeArrayPath(path)] = struct{}{}
			}
			visit(n.Node, n)
			visit(n.Property, n)
		case *exprast.UnaryNode:
			visit(n.Node, n)
		case *exprast.BinaryNode:
			visit(n.Left, n)
			visit(n.Right, n)
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
		case *exprast.CallNode:
			visit(n.Callee, n)
			for _, arg := range n.Arguments {
				visit(arg, n)
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
	return paths
}

func memberPath(node *exprast.MemberNode) (string, bool) {
	if node == nil {
		return "", false
	}

	base, ok := memberBase(node.Node)
	if !ok {
		return "", false
	}

	prop, ok := memberProperty(node.Property)
	if !ok {
		return "", false
	}

	if strings.HasPrefix(prop, "[") {
		return base + prop, true
	}
	return base + "." + prop, true
}

func memberBase(node exprast.Node) (string, bool) {
	switch n := node.(type) {
	case *exprast.IdentifierNode:
		return n.Value, true
	case *exprast.MemberNode:
		return memberPath(n)
	case *exprast.PointerNode:
		return n.Name, true
	default:
		return "", false
	}
}

func memberProperty(node exprast.Node) (string, bool) {
	switch n := node.(type) {
	case *exprast.StringNode:
		return n.Value, true
	case *exprast.IdentifierNode:
		return n.Value, true
	case *exprast.IntegerNode:
		return fmt.Sprintf("[%d]", n.Value), true
	default:
		return "", false
	}
}

func normalizeArrayPath(path string) string {
	return arrayIndexPattern.ReplaceAllString(path, "[]")
}
