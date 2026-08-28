package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentedCompilerCommandsExistAndHelpIsSorted(t *testing.T) {
	commands = make(map[string]*Command)
	defineCommands()

	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "COMMANDS.md"))
	require.NoError(t, err)
	compilerSection := strings.SplitN(string(data), "## effectusd - Runtime Daemon", 2)[0]
	headings := regexp.MustCompile(`(?m)^#### ([a-z][a-z0-9-]+)$`).FindAllStringSubmatch(compilerSection, -1)
	require.NotEmpty(t, headings)
	seen := make(map[string]struct{}, len(headings))
	for _, heading := range headings {
		name := heading[1]
		_, duplicate := seen[name]
		require.Falsef(t, duplicate, "compiler command %s is documented more than once", name)
		seen[name] = struct{}{}
		_, ok := commands[name]
		require.Truef(t, ok, "documented compiler command %s has no executable definition", name)
	}
	require.Contains(t, compilerSection, "effectusc migrate-workflows [--output workflow.effx] legacy-workflows.json")

	names := sortedCommandNames()
	expected := append([]string(nil), names...)
	sort.Strings(expected)
	require.Equal(t, expected, names)
	require.Contains(t, names, "migrate-workflows")
}
