package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/josephjohncox/effectus/internal/guardrails"
	"github.com/stretchr/testify/require"
)

func TestRepositoryModuleInventoryContainsOnlyRootModule(t *testing.T) {
	root, err := repositoryRoot()
	require.NoError(t, err)

	modules, err := guardrails.DiscoverModules(root)
	require.NoError(t, err)
	require.Equal(t, []string{"."}, modules)
}

func TestRepositoryBudgetCountsMatchInventories(t *testing.T) {
	root, err := repositoryRoot()
	require.NoError(t, err)

	counts, err := budgetInventoryCounts(root)
	require.NoError(t, err)
	budgets, err := os.ReadFile(filepath.Join(root, "guardrails", "BUDGETS.md"))
	require.NoError(t, err)
	require.Empty(t, guardrails.CheckBudgetCounts(string(budgets), counts))
}

func TestDocumentationClaimsRejectsPublishedV030CompatibilityPathsInEveryMarkdownDocument(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	releases := filepath.Join(docs, "releases")
	require.NoError(t, os.MkdirAll(releases, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "examples"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(docs, "COMPATIBILITY.md"), []byte("The first future root release that contains this branch introduces these paths.\nPublished `v0.3.0` does not contain these paths.\njust smoke-compat \"$ROOT_VERSION\"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(releases, "v0.3.0.md"), []byte("The v0.3.0 release provides compat/v03/invocation.\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("compat/v03/executorhttp is shipped by v0.3.0.\n"), 0o600))

	violations, err := documentationClaims(root)

	require.NoError(t, err)
	require.Equal(t, []string{
		"README.md: incorrect published compatibility claim: v0.3.0 affirmatively claims a compat/v03 path",
		"docs/releases/v0.3.0.md: incorrect published compatibility claim: v0.3.0 affirmatively claims a compat/v03 path",
	}, violations)
}

func TestDocumentationClaimsAllowsExplicitPublishedV030Negation(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	require.NoError(t, os.MkdirAll(docs, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(docs, "COMPATIBILITY.md"), []byte("The first future root release that contains this branch introduces these paths.\nPublished `v0.3.0` does not contain these paths.\njust smoke-compat \"$ROOT_VERSION\"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("Published v0.3.0 does not provide compat/v03/invocation.\n"), 0o600))

	violations, err := documentationClaims(root)

	require.NoError(t, err)
	require.Empty(t, violations)
}
