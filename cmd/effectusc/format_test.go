package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatCheckDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unformatted.eff")
	original := []byte(`rule "ready" priority 1 { when { system.ready == true } then { } }`)
	require.NoError(t, os.WriteFile(path, original, 0o600))
	before := sha256.Sum256(original)

	command := newFormatCommand()
	require.NoError(t, command.FlagSet.Parse([]string{"--check", path}))
	require.EqualError(t, command.Run(), "formatting required")

	afterBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	after := sha256.Sum256(afterBytes)
	require.Equal(t, before, after, "format --check must not mutate input")
}
