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

type factsReport struct {
	Coverage analysis.CoverageReport `json:"coverage"`
	Counts   factsCounts             `json:"counts"`
}

type factsCounts struct {
	Used    int `json:"used"`
	Unknown int `json:"unknown"`
	Unused  int `json:"unused"`
	Known   int `json:"known"`
}

func newFactsCommand() *Command {
	factsCmd := &Command{
		Name:        "facts",
		Description: "Report fact coverage for rules/flows",
		FlagSet:     flag.NewFlagSet("facts", flag.ExitOnError),
	}

	schemaFiles := factsCmd.FlagSet.String("schema", "", "Comma-separated list of schema files to load")
	schemaSources := factsCmd.FlagSet.String("schema-sources", "", "Path to schema sources config (YAML/JSON)")
	format := factsCmd.FlagSet.String("format", "text", "Output format: text or json")
	output := factsCmd.FlagSet.String("output", "", "Output file (defaults to stdout)")
	verbose := factsCmd.FlagSet.Bool("verbose", false, "Show detailed output")

	factsCmd.Run = func() error {
		files := factsCmd.FlagSet.Args()
		if len(files) < 1 {
			return fmt.Errorf("no input files specified")
		}

		_, typeSystem := createEmptyFacts(*schemaFiles, *verbose)
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

		combined := analysis.CoverageReport{}
		for _, filename := range files {
			parsed, err := comp.ParseFile(filename)
			if err != nil {
				return fmt.Errorf("parsing %s: %w", filename, err)
			}
			graph := analysis.BuildDependencyGraph(parsed, knownFacts)
			combined = mergeCoverageReports(combined, graph.Coverage)
		}

		report := factsReport{
			Coverage: combined,
			Counts: factsCounts{
				Used:    len(combined.UsedFacts),
				Unknown: len(combined.UnknownFacts),
				Unused:  len(combined.UnusedFacts),
				Known:   len(knownFacts),
			},
		}

		switch strings.ToLower(strings.TrimSpace(*format)) {
		case "json":
			payload, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return fmt.Errorf("encoding report: %w", err)
			}
			return outputBytesFacts(payload, *output)
		case "text":
			return outputBytesFacts([]byte(formatFactsReport(report)), *output)
		default:
			return fmt.Errorf("unsupported format: %s", *format)
		}
	}

	return factsCmd
}

func mergeCoverageReports(a, b analysis.CoverageReport) analysis.CoverageReport {
	return analysis.CoverageReport{
		UsedFacts:    mergeCoverageList(a.UsedFacts, b.UsedFacts),
		UnknownFacts: mergeCoverageList(a.UnknownFacts, b.UnknownFacts),
		UnusedFacts:  mergeCoverageList(a.UnusedFacts, b.UnusedFacts),
	}
}

func mergeCoverageList(a, b []string) []string {
	set := make(map[string]struct{})
	for _, value := range a {
		set[value] = struct{}{}
	}
	for _, value := range b {
		set[value] = struct{}{}
	}
	merged := make([]string, 0, len(set))
	for value := range set {
		merged = append(merged, value)
	}
	sort.Strings(merged)
	return merged
}

func formatFactsReport(report factsReport) string {
	var b strings.Builder
	b.WriteString("Fact coverage\n")
	b.WriteString(fmt.Sprintf("Known: %d  Used: %d  Unknown: %d  Unused: %d\n\n",
		report.Counts.Known, report.Counts.Used, report.Counts.Unknown, report.Counts.Unused))

	writeListSection(&b, "Used facts", report.Coverage.UsedFacts)
	writeListSection(&b, "Unknown facts", report.Coverage.UnknownFacts)
	writeListSection(&b, "Unused facts", report.Coverage.UnusedFacts)

	return b.String()
}

func writeListSection(b *strings.Builder, title string, items []string) {
	b.WriteString(title)
	b.WriteString(":\n")
	if len(items) == 0 {
		b.WriteString("  (none)\n\n")
		return
	}
	for _, item := range items {
		b.WriteString("  - ")
		b.WriteString(item)
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func outputBytesFacts(data []byte, output string) error {
	if output == "" {
		fmt.Println(string(data))
		return nil
	}
	return os.WriteFile(output, data, 0644)
}
