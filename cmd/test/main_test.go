package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileProgramMixedInputsHasDeterministicRequiredFacts(t *testing.T) {
	dir := t.TempDir()
	listPath := filepath.Join(dir, "rules.eff")
	flowPath := filepath.Join(dir, "flow.effx")
	require.NoError(t, os.WriteFile(listPath, []byte(`rule "list" priority 1 { when { customer.ready } then {} }`), 0o600))
	require.NoError(t, os.WriteFile(flowPath, []byte(`flow "flow" priority 1 { when { order.ready } steps {} }`), 0o600))

	first, err := compileProgram([]string{listPath, flowPath})
	require.NoError(t, err)
	second, err := compileProgram([]string{flowPath, listPath})
	require.NoError(t, err)
	require.Equal(t, []string{"customer.ready", "order.ready"}, first.RequiredFacts())
	require.Equal(t, first.RequiredFacts(), second.RequiredFacts())
}
