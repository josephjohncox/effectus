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
	for _, heading := range headings {
		_, ok := commands[heading[1]]
		require.Truef(t, ok, "documented compiler command %s has no executable definition", heading[1])
	}

	names := sortedCommandNames()
	expected := append([]string(nil), names...)
	sort.Strings(expected)
	require.Equal(t, expected, names)
	require.Contains(t, names, "migrate-workflows")
}
