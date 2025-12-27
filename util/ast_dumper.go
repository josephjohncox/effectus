package util

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/effectus/effectus-go/ast"
)

// ASTDumper dumps AST structures to a writer
type ASTDumper struct {
	writer io.Writer
	indent string
}

// NewASTDumper creates a new AST dumper that writes to the given writer
func NewASTDumper(writer io.Writer) *ASTDumper {
	return &ASTDumper{
		writer: writer,
		indent: "",
	}
}

// NewStdoutASTDumper creates a new AST dumper that writes to stdout
func NewStdoutASTDumper() *ASTDumper {
	return NewASTDumper(os.Stdout)
}

// DumpFile dumps an entire AST file
func (d *ASTDumper) DumpFile(file *ast.File) {
	d.indent = ""
	fmt.Fprintf(d.writer, "File:\n")

	// Dump flows
	d.DumpFlows(file.Flows)

	// Dump rules
	d.DumpRules(file.Rules)
}

// DumpFlows dumps all flows in a file
func (d *ASTDumper) DumpFlows(flows []*ast.Flow) {
	if len(flows) == 0 {
		return
	}

	fmt.Fprintf(d.writer, "%sFlows (%d):\n", d.indent, len(flows))
	for i, flow := range flows {
		fmt.Fprintf(d.writer, "%s  Flow %d: %s (Priority: %d)\n", d.indent, i+1, flow.Name, flow.Priority)

		// Dump when predicate
		if flow.When != nil && flow.When.Expression != "" {
			fmt.Fprintf(d.writer, "%s    When: %s\n", d.indent, flow.When.Expression)
		}

		// Dump steps
		d.dumpSteps(flow.Steps, d.indent+"    ")
	}
}

// DumpRules dumps all rules in a file
func (d *ASTDumper) DumpRules(rules []*ast.Rule) {
	if len(rules) == 0 {
		return
	}

	fmt.Fprintf(d.writer, "%sRules (%d):\n", d.indent, len(rules))
	for i, rule := range rules {
		fmt.Fprintf(d.writer, "%s  Rule %d: %s (Priority: %d)\n", d.indent, i+1, rule.Name, rule.Priority)

		// Dump all when-then blocks
		for j, block := range rule.Blocks {
			fmt.Fprintf(d.writer, "%s    Block %d:\n", d.indent, j+1)

			// Dump when predicate
			fmt.Printf("block: %v\n", block.When)
			if block.When != nil && block.When.Expression != "" {
				fmt.Fprintf(d.writer, "%s      When: %s\n", d.indent, block.When.Expression)
			}

			// Dump then effects
			d.dumpEffects(block.Then, d.indent+"      ")
		}
	}
}

// dumpPredicates dumps the predicate block (for compatibility with older code)
func (d *ASTDumper) dumpPredicates(when *ast.PredicateBlock, indentStr string) {
	if when == nil || when.Expression == "" {
		return
	}

	fmt.Fprintf(d.writer, "%sPredicates: %s\n", indentStr, when.Expression)
}

// dumpSteps dumps the steps block
func (d *ASTDumper) dumpSteps(steps *ast.StepBlock, indentStr string) {
	if steps == nil || steps.Steps == nil {
		return
	}

	fmt.Fprintf(d.writer, "%sSteps (%d):\n", indentStr, len(steps.Steps))
	for i, step := range steps.Steps {
		fmt.Fprintf(d.writer, "%s  Step %d: %s\n", indentStr, i+1, step.Verb)
		d.dumpArgs(step.Args, indentStr+"  ")
		if step.BindName != "" {
			fmt.Fprintf(d.writer, "%s    BindName: %s\n", indentStr, step.BindName)
		}
	}
}

// dumpEffects dumps the effects block
func (d *ASTDumper) dumpEffects(effects *ast.EffectBlock, indentStr string) {
	if effects == nil || effects.Effects == nil {
		return
	}

	fmt.Fprintf(d.writer, "%sEffects (%d):\n", indentStr, len(effects.Effects))
	for i, effect := range effects.Effects {
		effectDesc := effect.Verb
		if effect.BindName != "" {
			effectDesc = effect.BindName + " = " + effect.Verb
		}
		fmt.Fprintf(d.writer, "%s  Effect %d: %s\n", indentStr, i+1, effectDesc)
		d.dumpNamedArgs(effect.Args, indentStr+"  ")
	}
}

// dumpNamedArgs dumps named arguments
func (d *ASTDumper) dumpNamedArgs(args []*ast.StepArg, indentStr string) {
	if len(args) == 0 {
		return
	}

	fmt.Fprintf(d.writer, "%sArgs (%d):\n", indentStr, len(args))
	for j, arg := range args {
		fmt.Fprintf(d.writer, "%s  Arg %d: %s\n", indentStr, j+1, arg.Name)
		if arg.Value != nil {
			if arg.Value.VarRef != "" {
				fmt.Fprintf(d.writer, "%s    VarRef: %s\n", indentStr, arg.Value.VarRef)
			} else if arg.Value.PathExpr != nil {
				pathInfo := arg.Value.PathExpr.GetFullPath()
				// Add details about the Path field if it exists
				if arg.Value.PathExpr.Path != "" {
					pathInfo = fmt.Sprintf("%s ", arg.Value.PathExpr.Path)
				}
				fmt.Fprintf(d.writer, "%s    FactPath: %s\n", indentStr, pathInfo)
			} else if arg.Value.Literal != nil {
				fmt.Fprintf(d.writer, "%s    Literal: %s\n", indentStr, describeLiteral(arg.Value.Literal))
			}
		}
	}
}

// For backward compatibility
func (d *ASTDumper) dumpArgs(args []*ast.StepArg, indentStr string) {
	d.dumpNamedArgs(args, indentStr)
}

// Helper function to describe literals
func describeLiteral(lit *ast.Literal) string {
	if lit == nil {
		return "nil"
	}

	if lit.String != nil {
		return fmt.Sprintf("string(%s)", *lit.String)
	}
	if lit.Int != nil {
		return fmt.Sprintf("int(%d)", *lit.Int)
	}
	if lit.Float != nil {
		return fmt.Sprintf("float(%f)", *lit.Float)
	}
	if lit.Bool != nil {
		return fmt.Sprintf("bool(%t)", *lit.Bool)
	}
	if lit.List != nil {
		return fmt.Sprintf("list(%s)", describeLiteralList(lit.List))
	}
	if lit.Map != nil {
		return fmt.Sprintf("map(%s)", describeLiteralMap(lit.Map))
	}
	return "unknown"
}

// Helper function to describe a list of literals
func describeLiteralList(list []ast.Literal) string {
	elements := make([]string, len(list))
	for i, item := range list {
		elements[i] = describeLiteral(&item)
	}
	return "[" + strings.Join(elements, ", ") + "]"
}

// Helper function to describe a map of literals
func describeLiteralMap(entries []*ast.MapEntry) string {
	if len(entries) == 0 {
		return "{}"
	}

	elements := make([]string, len(entries))
	for i, entry := range entries {
		elements[i] = fmt.Sprintf("%s: %s", entry.Key, describeLiteral(&entry.Value))
	}
	return "{" + strings.Join(elements, ", ") + "}"
}

// dumpArg dumps a single argument
func (d *ASTDumper) dumpArg(arg *ast.StepArg, indentStr string) {
	if arg == nil {
		return
	}

	fmt.Fprintf(d.writer, "%sArg %s:\n", indentStr, arg.Name)
	if arg.Value != nil {
		if arg.Value.VarRef != "" {
			fmt.Fprintf(d.writer, "%s    VarRef: %s\n", indentStr, arg.Value.VarRef)
		} else if arg.Value.PathExpr != nil {
			pathInfo := arg.Value.PathExpr.GetFullPath()
			// Add details about the Path if it exists
			if arg.Value.PathExpr.Path != "" {
				pathInfo = fmt.Sprintf("%s ", arg.Value.PathExpr.Path)
			}
			fmt.Fprintf(d.writer, "%s    Path: %s\n", indentStr, pathInfo)
		} else if arg.Value.Literal != nil {
			fmt.Fprintf(d.writer, "%s    Literal: %s\n", indentStr, describeLiteral(arg.Value.Literal))
		}
	}
}
