package guardrails

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentedContributorRecipesExist(t *testing.T) {
	root := "../.."
	data, err := os.ReadFile(filepath.Join(root, "justfile"))
	require.NoError(t, err)
	recipes := make(map[string]bool)
	for _, recipe := range ParseRecipes(string(data)) {
		recipes[recipe] = true
	}
	reference := regexp.MustCompile(`\bjust[ \t]+([a-z][a-z0-9-]*)`)
	for _, name := range []string{"AGENTS.md", "CONTRIBUTING.md"} {
		t.Run(name, func(t *testing.T) {
			text, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			matches := reference.FindAllStringSubmatch(string(text), -1)
			require.NotEmpty(t, matches)
			for _, match := range matches {
				require.True(t, recipes[match[1]], "documented recipe %s is absent from justfile", match[1])
			}
		})
	}
}
