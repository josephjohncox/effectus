package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/effectus/effectus-go/lint"
	"github.com/effectus/effectus-go/schema/verb"
)

var positionPattern = regexp.MustCompile(`:(\d+):(\d+)`)

func splitCommaList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func issueFromError(file string, err error) lint.Issue {
	pos := positionFromError(err)
	return lint.Issue{
		File:     file,
		Pos:      pos,
		Severity: "error",
		Code:     "typecheck",
		Message:  err.Error(),
	}
}

func positionFromError(err error) lexer.Position {
	if err == nil {
		return lexer.Position{}
	}

	matches := positionPattern.FindAllStringSubmatch(err.Error(), -1)
	if len(matches) == 0 {
		return lexer.Position{}
	}

	last := matches[len(matches)-1]
	if len(last) < 3 {
		return lexer.Position{}
	}

	line, _ := strconv.Atoi(last[1])
	col, _ := strconv.Atoi(last[2])

	return lexer.Position{Line: line, Column: col}
}

func formatIssuesText(issues []lint.Issue) string {
	sorted := append([]lint.Issue{}, issues...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].File != sorted[j].File {
			return sorted[i].File < sorted[j].File
		}
		if sorted[i].Pos.Line != sorted[j].Pos.Line {
			return sorted[i].Pos.Line < sorted[j].Pos.Line
		}
		return sorted[i].Pos.Column < sorted[j].Pos.Column
	})

	lines := make([]string, 0, len(sorted))
	for _, issue := range sorted {
		line := issue.Pos.Line
		col := issue.Pos.Column
		if line <= 0 {
			line = 1
		}
		if col <= 0 {
			col = 1
		}
		lines = append(lines, fmt.Sprintf("%s:%d:%d [%s] %s", issue.File, line, col, issue.Code, issue.Message))
	}

	return strings.Join(lines, "\n")
}

func loadVerbRegistry(files []string, verbose bool) *verb.Registry {
	if len(files) == 0 {
		return nil
	}

	registry := verb.NewRegistry(nil)
	for _, file := range files {
		if verbose {
			fmt.Printf("Loading verb registry from %s...\n", file)
		}
		if err := registry.LoadFromJSON(file); err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: %s could not be loaded as verb registry (%v)\n", file, err)
			}
		}
	}

	if registry.Count() == 0 {
		return nil
	}

	return registry
}
