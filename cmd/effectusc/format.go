package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/compiler"
)

func newFormatCommand() *Command {
	formatCmd := &Command{
		Name:        "format",
		Description: "Format Effectus rule files",
		FlagSet:     flag.NewFlagSet("format", flag.ExitOnError),
	}

	write := formatCmd.FlagSet.Bool("write", true, "Write formatted output back to files")
	stdout := formatCmd.FlagSet.Bool("stdout", false, "Print formatted output to stdout")
	check := formatCmd.FlagSet.Bool("check", false, "Return non-zero exit code if files need formatting")

	formatCmd.Run = func() error {
		files := formatCmd.FlagSet.Args()
		if len(files) < 1 {
			return fmt.Errorf("no input files specified")
		}

		comp := compiler.NewCompiler()
		needsChanges := false

		for _, filename := range files {
			parsed, err := comp.ParseFile(filename)
			if err != nil {
				return fmt.Errorf("parsing %s: %w", filename, err)
			}

			formatted := formatEffectusFile(parsed)
			original, err := os.ReadFile(filename)
			if err != nil {
				return fmt.Errorf("reading %s: %w", filename, err)
			}

			if string(original) != formatted {
				needsChanges = true
			}

			if *stdout {
				fmt.Println(formatted)
				continue
			}

			if *write {
				if err := os.WriteFile(filename, []byte(formatted), 0644); err != nil {
					return fmt.Errorf("writing %s: %w", filename, err)
				}
			}
		}

		if *check && needsChanges {
			return fmt.Errorf("formatting required")
		}
		return nil
	}

	return formatCmd
}

func formatEffectusFile(file *ast.File) string {
	if file == nil {
		return ""
	}

	var b strings.Builder
	for _, rule := range file.Rules {
		formatRule(&b, rule, 0)
		b.WriteString("\n")
	}
	for _, flow := range file.Flows {
		formatFlow(&b, flow, 0)
		b.WriteString("\n")
	}

	result := strings.TrimRight(b.String(), "\n")
	return result + "\n"
}

func formatRule(b *strings.Builder, rule *ast.Rule, indent int) {
	if rule == nil {
		return
	}
	writeIndent(b, indent)
	fmt.Fprintf(b, "rule %q priority %d {\n", rule.Name, rule.Priority)

	for _, block := range rule.Blocks {
		if block.When != nil {
			writeIndent(b, indent+2)
			b.WriteString("when {\n")
			writeIndent(b, indent+4)
			b.WriteString(strings.TrimSpace(block.When.Expression))
			b.WriteString("\n")
			writeIndent(b, indent+2)
			b.WriteString("}\n")
		}
		if block.Then != nil {
			writeIndent(b, indent+2)
			b.WriteString("then {\n")
			for _, effect := range block.Then.Effects {
				writeIndent(b, indent+4)
				formatEffect(b, effect)
				b.WriteString("\n")
			}
			writeIndent(b, indent+2)
			b.WriteString("}\n")
		}
	}

	writeIndent(b, indent)
	b.WriteString("}\n")
}

func formatFlow(b *strings.Builder, flow *ast.Flow, indent int) {
	if flow == nil {
		return
	}
	writeIndent(b, indent)
	fmt.Fprintf(b, "flow %q priority %d {\n", flow.Name, flow.Priority)

	if flow.When != nil {
		writeIndent(b, indent+2)
		b.WriteString("when {\n")
		writeIndent(b, indent+4)
		b.WriteString(strings.TrimSpace(flow.When.Expression))
		b.WriteString("\n")
		writeIndent(b, indent+2)
		b.WriteString("}\n")
	}

	if flow.Steps != nil {
		writeIndent(b, indent+2)
		b.WriteString("steps {\n")
		for _, step := range flow.Steps.Steps {
			writeIndent(b, indent+4)
			formatStep(b, step)
			b.WriteString("\n")
		}
		writeIndent(b, indent+2)
		b.WriteString("}\n")
	}

	writeIndent(b, indent)
	b.WriteString("}\n")
}

func formatEffect(b *strings.Builder, effect *ast.Effect) {
	if effect == nil {
		return
	}
	if effect.BindName != "" {
		b.WriteString(effect.BindName)
		b.WriteString(" = ")
	}
	b.WriteString(effect.Verb)
	b.WriteString("(")
	formatArgs(b, effect.Args)
	b.WriteString(")")
}

func formatStep(b *strings.Builder, step *ast.Step) {
	if step == nil {
		return
	}
	if step.BindName != "" {
		b.WriteString(step.BindName)
		b.WriteString(" = ")
	}
	b.WriteString(step.Verb)
	b.WriteString("(")
	formatArgs(b, step.Args)
	b.WriteString(")")
	if step.Arrow != "" {
		b.WriteString(" -> ")
		b.WriteString(step.Arrow)
	}
}

func formatArgs(b *strings.Builder, args []*ast.StepArg) {
	for i, arg := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(arg.Name)
		b.WriteString(": ")
		b.WriteString(formatArgValue(arg.Value))
	}
}

func formatArgValue(value *ast.ArgValue) string {
	if value == nil {
		return "null"
	}
	if value.VarRef != "" {
		return value.VarRef
	}
	if value.PathExpr != nil {
		return value.PathExpr.GetFullPath()
	}
	if value.Literal != nil {
		return formatLiteral(value.Literal)
	}
	return "null"
}

func formatLiteral(lit *ast.Literal) string {
	if lit == nil {
		return "null"
	}
	if lit.String != nil {
		raw := *lit.String
		if unquoted, err := strconv.Unquote(raw); err == nil {
			return strconv.Quote(unquoted)
		}
		return strconv.Quote(raw)
	}
	if lit.Int != nil {
		return fmt.Sprintf("%d", *lit.Int)
	}
	if lit.Float != nil {
		return fmt.Sprintf("%v", *lit.Float)
	}
	if lit.Bool != nil {
		if *lit.Bool {
			return "true"
		}
		return "false"
	}
	if lit.List != nil {
		items := make([]string, 0, len(lit.List))
		for _, item := range lit.List {
			items = append(items, formatLiteral(&item))
		}
		return "[" + strings.Join(items, ", ") + "]"
	}
	if lit.Map != nil {
		entries := make([]string, 0, len(lit.Map))
		for _, entry := range lit.Map {
			if entry == nil {
				continue
			}
			entries = append(entries, fmt.Sprintf("%s: %s", entry.Key, formatLiteral(&entry.Value)))
		}
		return "{" + strings.Join(entries, ", ") + "}"
	}
	return "null"
}

func writeIndent(b *strings.Builder, indent int) {
	if indent <= 0 {
		return
	}
	b.WriteString(strings.Repeat(" ", indent))
}
