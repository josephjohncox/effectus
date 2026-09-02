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

func TestDocumentedCompilerCommandsMatchExecutableInventoryAndHelpIsSorted(t *testing.T) {
	commands = make(map[string]*Command)
	defineCommands()

	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "COMMANDS.md"))
	require.NoError(t, err)
	compilerSection := strings.SplitN(string(data), "## effectusd - Runtime Daemon", 2)[0]
	headings := regexp.MustCompile(`(?m)^#### ([a-z][a-z0-9-]+)$`).FindAllStringSubmatch(compilerSection, -1)
	require.NotEmpty(t, headings)
	documented := make([]string, 0, len(headings))
	seen := make(map[string]struct{}, len(headings))
	for _, heading := range headings {
		name := heading[1]
		_, duplicate := seen[name]
		require.Falsef(t, duplicate, "compiler command %s is documented more than once", name)
		seen[name] = struct{}{}
		documented = append(documented, name)
	}
	sort.Strings(documented)

	names := sortedCommandNames()
	expected := append([]string(nil), names...)
	sort.Strings(expected)
	require.Equal(t, expected, names, "help command order must be deterministic")
	require.Equal(t, names, documented, "docs/COMMANDS.md must document every executable compiler command exactly once")
	require.Contains(t, compilerSection, "effectusc migrate-workflows [--output workflow.effx] legacy-workflows.json")
}

func TestIntegrationGuideSeparatesPublishedV03FromCurrentEmbeddedAPI(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "INTEGRATION.md"))
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "Published `v0.3.0` is not that release")
	require.Contains(t, text, "EFFECTUS_VERSION")
	require.Contains(t, text, `go get github.com/josephjohncox/effectus@"${EFFECTUS_VERSION}"`)
	require.NotContains(t, text, "effectus@v0.3.0")
	require.NotContains(t, text, "effectus@main")
}

func TestPublishedEffectuscInvocationsUseExecutableCommands(t *testing.T) {
	commands = make(map[string]*Command)
	defineCommands()
	root := filepath.Join("..", "..")
	invocation := regexp.MustCompile(`(?:go run \./cmd/)?effectusc[[:space:]]+([a-z][a-z0-9-]+)`)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root {
			switch entry.Name() {
			case ".git", ".pi", ".codebase-index", "node_modules", "site":
				return filepath.SkipDir
			}
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range invocation.FindAllStringSubmatch(string(data), -1) {
			_, exists := commands[match[1]]
			require.Truef(t, exists, "%s documents unknown effectusc command %s", path, match[1])
		}
		return nil
	}))
}
