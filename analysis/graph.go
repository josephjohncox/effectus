package analysis

import (
	"regexp"
	"sort"

	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/schema"
)

// Edge represents a dependency edge in the graph.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// CoverageReport summarizes fact usage coverage.
type CoverageReport struct {
	UsedFacts    []string `json:"used_facts"`
	UnknownFacts []string `json:"unknown_facts"`
	UnusedFacts  []string `json:"unused_facts"`
}

// DependencyGraph captures rules/flows usage of facts and verbs.
type DependencyGraph struct {
	Rules    []string       `json:"rules"`
	Flows    []string       `json:"flows"`
	Facts    []string       `json:"facts"`
	Verbs    []string       `json:"verbs"`
	Edges    []Edge         `json:"edges"`
	Coverage CoverageReport `json:"coverage"`
}

// BuildDependencyGraph builds a dependency graph and coverage report.
func BuildDependencyGraph(file *ast.File, knownFacts map[string]struct{}) DependencyGraph {
	graph := DependencyGraph{}
	if file == nil {
		return graph
	}

	usedFacts := make(map[string]struct{})
	usedVerbs := make(map[string]struct{})

	for _, rule := range file.Rules {
		graph.Rules = append(graph.Rules, rule.Name)
		facts, verbs := collectRuleUsage(rule)
		for fact := range facts {
			usedFacts[fact] = struct{}{}
			graph.Edges = append(graph.Edges, Edge{
				From: "rule:" + rule.Name,
				To:   "fact:" + fact,
				Kind: "fact",
			})
		}
		for verb := range verbs {
			usedVerbs[verb] = struct{}{}
			graph.Edges = append(graph.Edges, Edge{
				From: "rule:" + rule.Name,
				To:   "verb:" + verb,
				Kind: "verb",
			})
		}
	}

	for _, flow := range file.Flows {
		graph.Flows = append(graph.Flows, flow.Name)
		facts, verbs := collectFlowUsage(flow)
		for fact := range facts {
			usedFacts[fact] = struct{}{}
			graph.Edges = append(graph.Edges, Edge{
				From: "flow:" + flow.Name,
				To:   "fact:" + fact,
				Kind: "fact",
			})
		}
		for verb := range verbs {
			usedVerbs[verb] = struct{}{}
			graph.Edges = append(graph.Edges, Edge{
				From: "flow:" + flow.Name,
				To:   "verb:" + verb,
				Kind: "verb",
			})
		}
	}

	graph.Facts = sortedKeys(usedFacts)
	graph.Verbs = sortedKeys(usedVerbs)

	graph.Coverage = buildCoverageReport(usedFacts, knownFacts)
	return graph
}

func buildCoverageReport(usedFacts, knownFacts map[string]struct{}) CoverageReport {
	report := CoverageReport{}

	for fact := range usedFacts {
		report.UsedFacts = append(report.UsedFacts, fact)
		if _, ok := knownFacts[fact]; !ok {
			report.UnknownFacts = append(report.UnknownFacts, fact)
		}
	}

	for fact := range knownFacts {
		if _, ok := usedFacts[fact]; !ok {
			report.UnusedFacts = append(report.UnusedFacts, fact)
		}
	}

	sort.Strings(report.UsedFacts)
	sort.Strings(report.UnknownFacts)
	sort.Strings(report.UnusedFacts)
	return report
}

func collectRuleUsage(rule *ast.Rule) (map[string]struct{}, map[string]struct{}) {
	facts := make(map[string]struct{})
	verbs := make(map[string]struct{})
	if rule == nil {
		return facts, verbs
	}

	for _, block := range rule.Blocks {
		if block.When != nil {
			for path := range schema.ExtractFactPaths(block.When.Expression) {
				facts[normalizeArrayPath(path)] = struct{}{}
			}
		}
		if block.Then != nil {
			for _, effect := range block.Then.Effects {
				verbs[effect.Verb] = struct{}{}
				for _, arg := range effect.Args {
					if arg.Value != nil && arg.Value.PathExpr != nil {
						facts[normalizeArrayPath(arg.Value.PathExpr.GetFullPath())] = struct{}{}
					}
				}
			}
		}
	}

	return facts, verbs
}

func collectFlowUsage(flow *ast.Flow) (map[string]struct{}, map[string]struct{}) {
	facts := make(map[string]struct{})
	verbs := make(map[string]struct{})
	if flow == nil {
		return facts, verbs
	}

	if flow.When != nil {
		for path := range schema.ExtractFactPaths(flow.When.Expression) {
			facts[normalizeArrayPath(path)] = struct{}{}
		}
	}

	if flow.Steps != nil {
		for _, step := range flow.Steps.Steps {
			verbs[step.Verb] = struct{}{}
			for _, arg := range step.Args {
				if arg.Value != nil && arg.Value.PathExpr != nil {
					facts[normalizeArrayPath(arg.Value.PathExpr.GetFullPath())] = struct{}{}
				}
			}
		}
	}

	return facts, verbs
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var indexPattern = regexp.MustCompile(`\[[0-9]+\]`)

func normalizeArrayPath(path string) string {
	if path == "" {
		return path
	}
	return indexPattern.ReplaceAllString(path, "[]")
}
