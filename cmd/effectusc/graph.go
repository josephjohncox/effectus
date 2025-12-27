package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/effectus/effectus-go/analysis"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/internal/schemasources"
)

func newGraphCommand() *Command {
	graphCmd := &Command{
		Name:        "graph",
		Description: "Emit a dependency graph for rules/flows",
		FlagSet:     flag.NewFlagSet("graph", flag.ExitOnError),
	}

	schemaFiles := graphCmd.FlagSet.String("schema", "", "Comma-separated list of schema files to load")
	schemaSources := graphCmd.FlagSet.String("schema-sources", "", "Path to schema sources config (YAML/JSON)")
	format := graphCmd.FlagSet.String("format", "json", "Output format: json or dot")
	output := graphCmd.FlagSet.String("output", "", "Output file (defaults to stdout)")
	verbose := graphCmd.FlagSet.Bool("verbose", false, "Show detailed output")

	graphCmd.Run = func() error {
		files := graphCmd.FlagSet.Args()
		if len(files) < 1 {
			return fmt.Errorf("no input files specified")
		}

		facts, typeSystem := createEmptyFacts(*schemaFiles, *verbose)
		if strings.TrimSpace(*schemaSources) != "" {
			sources, err := schemasources.LoadFromFile(*schemaSources)
			if err != nil {
				return err
			}
			if err := schemasources.Apply(context.Background(), typeSystem, sources, *verbose); err != nil {
				return err
			}
		}
		knownFacts := make(map[string]struct{})
		for _, path := range typeSystem.GetAllFactPaths() {
			knownFacts[path] = struct{}{}
		}

		comp := compiler.NewCompiler()
		_ = facts // keep parity for future use

		var combined analysis.DependencyGraph
		for _, filename := range files {
			parsed, err := comp.ParseFile(filename)
			if err != nil {
				return fmt.Errorf("parsing %s: %w", filename, err)
			}
			graph := analysis.BuildDependencyGraph(parsed, knownFacts)
			combined = mergeGraphs(combined, graph)
		}

		switch strings.ToLower(*format) {
		case "json":
			payload, err := json.MarshalIndent(combined, "", "  ")
			if err != nil {
				return fmt.Errorf("encoding graph: %w", err)
			}
			return outputBytes(payload, *output)
		case "dot":
			dot := renderDOT(combined)
			return outputBytes([]byte(dot), *output)
		default:
			return fmt.Errorf("unsupported format: %s", *format)
		}
	}

	return graphCmd
}

func mergeGraphs(a, b analysis.DependencyGraph) analysis.DependencyGraph {
	result := analysis.DependencyGraph{}

	ruleSet := mergeStringSets(a.Rules, b.Rules)
	flowSet := mergeStringSets(a.Flows, b.Flows)
	factSet := mergeStringSets(a.Facts, b.Facts)
	verbSet := mergeStringSets(a.Verbs, b.Verbs)

	result.Rules = keysFromSet(ruleSet)
	result.Flows = keysFromSet(flowSet)
	result.Facts = keysFromSet(factSet)
	result.Verbs = keysFromSet(verbSet)

	edgeSet := make(map[analysis.Edge]struct{})
	for _, edge := range append(a.Edges, b.Edges...) {
		edgeSet[edge] = struct{}{}
	}
	for edge := range edgeSet {
		result.Edges = append(result.Edges, edge)
	}

	result.Coverage = analysis.CoverageReport{
		UsedFacts:    mergeCoverage(a.Coverage.UsedFacts, b.Coverage.UsedFacts),
		UnknownFacts: mergeCoverage(a.Coverage.UnknownFacts, b.Coverage.UnknownFacts),
		UnusedFacts:  mergeCoverage(a.Coverage.UnusedFacts, b.Coverage.UnusedFacts),
	}

	sort.Strings(result.Rules)
	sort.Strings(result.Flows)
	sort.Strings(result.Facts)
	sort.Strings(result.Verbs)
	sort.Slice(result.Edges, func(i, j int) bool {
		if result.Edges[i].From != result.Edges[j].From {
			return result.Edges[i].From < result.Edges[j].From
		}
		if result.Edges[i].To != result.Edges[j].To {
			return result.Edges[i].To < result.Edges[j].To
		}
		return result.Edges[i].Kind < result.Edges[j].Kind
	})

	return result
}

func mergeStringSets(a, b []string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, value := range a {
		set[value] = struct{}{}
	}
	for _, value := range b {
		set[value] = struct{}{}
	}
	return set
}

func keysFromSet(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mergeCoverage(a, b []string) []string {
	set := mergeStringSets(a, b)
	return keysFromSet(set)
}

func outputBytes(data []byte, output string) error {
	if output == "" {
		fmt.Println(string(data))
		return nil
	}
	return os.WriteFile(output, data, 0644)
}

func renderDOT(graph analysis.DependencyGraph) string {
	var b strings.Builder
	b.WriteString("digraph effectus {\n")
	b.WriteString("  rankdir=LR;\n")

	for _, rule := range graph.Rules {
		b.WriteString(fmt.Sprintf("  %q [shape=box,label=%q];\n", "rule:"+rule, rule))
	}
	for _, flow := range graph.Flows {
		b.WriteString(fmt.Sprintf("  %q [shape=box,label=%q,style=rounded];\n", "flow:"+flow, flow))
	}
	for _, fact := range graph.Facts {
		b.WriteString(fmt.Sprintf("  %q [shape=ellipse,label=%q];\n", "fact:"+fact, fact))
	}
	for _, verb := range graph.Verbs {
		b.WriteString(fmt.Sprintf("  %q [shape=diamond,label=%q];\n", "verb:"+verb, verb))
	}

	for _, edge := range graph.Edges {
		b.WriteString(fmt.Sprintf("  %q -> %q [label=%q];\n", edge.From, edge.To, edge.Kind))
	}

	b.WriteString("}\n")
	return b.String()
}
