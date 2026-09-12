package guardrails

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These source indexes use simple inline links. MkDocs checks links and anchors
// for the published pages; this also checks repository-only index targets.
func checkSourceIndexLinks(root, name, text string) error {
	links := regexp.MustCompile(`\[[^]\n]*\]\(([^)\n]+)\)`).FindAllStringSubmatch(text, -1)
	if len(links) == 0 {
		return fmt.Errorf("%s has no inline links", name)
	}
	for _, link := range links {
		target, err := url.Parse(link[1])
		if err != nil {
			return fmt.Errorf("%s: invalid link %q: %w", name, link[1], err)
		}
		if target.Scheme != "" || target.Host != "" || target.Path == "" {
			continue // Remote URLs and fragment-only links are not file-existence checks.
		}
		if filepath.IsAbs(target.Path) {
			return fmt.Errorf("%s: absolute source link %q", name, link[1])
		}
		path := filepath.Join(root, filepath.Dir(name), filepath.FromSlash(target.Path))
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s: source link leaves the repository: %q", name, link[1])
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s: link %q: %w", name, link[1], err)
		}
	}
	return nil
}

func TestDocumentedSourceIndexLinks(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	for _, name := range []string{"README.md", "docs/README.md", "AGENTS.md", "CONTRIBUTING.md"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			require.NoError(t, checkSourceIndexLinks(root, name, string(data)))
		})
	}
}

func TestDocumentedSourceIndexLinkFailures(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "target.md"), []byte("# Target\n"), 0o600))
	for _, test := range []struct {
		text string
		ok   bool
	}{
		{"[local](../target.md) [remote](https://example.invalid/no-network)", true},
		{"[encoded](../%74arget.md)", true},
		{"[missing](../absent.md)", false},
		{"[escape](../../outside.md)", false},
		{"[absolute](/target.md)", false},
		{"[invalid](%zz)", false},
		{"no links", false},
	} {
		err := checkSourceIndexLinks(root, "docs/README.md", test.text)
		if test.ok {
			require.NoError(t, err)
		} else {
			require.Error(t, err, test.text)
		}
	}
}

// Targeted stale-claim checks supplement behavioral tests and source review.
// They do not prove that every sentence in the documentation is accurate.
func TestDocumentedCurrentAndHistoricalClaims(t *testing.T) {
	for name, stale := range map[string][]string{
		"README.md":                     {"idempotency key or fencing token"},
		"docs/index.md":                 {"immutable runtime generations with atomic activation", "idempotency key or fencing token"},
		"docs/README.md":                {"defines strict YAML and JSON configuration", "defines activation, refresh"},
		"docs/GLOSSARY.md":              {"using the standard function library", "optional branching and bindings", "compensating verb that reverses"},
		"docs/releases/v0.4.0.md":       {"requires Docker Compose only"},
		"docs/LIFECYCLE.md":             {"An unknown destination outcome remains blocked rather than becoming an automatic retry."},
		"docs/coherent_flow.md":         {"Unknown destination outcomes block automatic progress. They do not become blind retries."},
		"docs/GUARANTEES.md":            {"An unknown external outcome is recorded for operator resolution;"},
		"docs/DURABLE_SAGA_PROTOCOL.md": {"Effectus retries an unknown outcome with the same idempotency key."},
		"docs/theory/README.md":         {"Legacy Go list and continuation APIs used by embedded applications"},
	} {
		data, err := os.ReadFile(filepath.Join("../..", name))
		require.NoError(t, err)
		for _, claim := range stale {
			require.NotContains(t, string(data), claim, name)
		}
	}
	for _, version := range []string{"v0.2.1", "v0.3.0"} {
		data, err := os.ReadFile(filepath.Join("../../docs/releases", version+".md"))
		require.NoError(t, err)
		require.Contains(t, string(data), "**Historical release note.**")
		require.Contains(t, string(data), "../COMMANDS.md", "historical commands must direct readers to the current reference")
	}
}
