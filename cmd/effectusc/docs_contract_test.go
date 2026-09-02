package main

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentedCLIAndFlags(t *testing.T) {
	documentation, err := os.ReadFile("../../docs/COMMANDS.md")
	require.NoError(t, err)
	text := string(documentation)
	commands := defineCommands()
	require.NotEmpty(t, commands, "documentation contract must have CLI commands to check")
	for name, command := range commands {
		require.Contains(t, text, "`"+name+"`", "documented command %q is missing", name)
		flagCount := 0
		command.flags.VisitAll(func(item *flag.Flag) {
			flagCount++
			require.Contains(t, text, "`--"+item.Name+"`", "documented flag --%s is missing", item.Name)
		})
		require.Positive(t, flagCount, "command %q has no documented flags", name)
	}
	for _, stale := range readNegativeFixtures(t, "testdata/docs") {
		require.NotContains(t, text, stale, "stale CLI surface %q remains documented", stale)
	}
}

func readNegativeFixtures(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.NotEmpty(t, entries, "negative documentation fixtures must not be empty")
	var values []string
	for _, entry := range entries {
		data, readErr := os.ReadFile(directory + "/" + entry.Name())
		require.NoError(t, readErr)
		value := strings.TrimSpace(string(data))
		require.NotEmpty(t, value, "negative fixture %s is empty", entry.Name())
		values = append(values, value)
	}
	return values
}
