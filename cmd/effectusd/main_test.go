package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/effectus/effectus-go/schema/verb"
	"github.com/stretchr/testify/require"
)

func TestLegacyDaemonFlagsAreRejectedBeforeFlagParsing(t *testing.T) {
	require.ErrorContains(t, rejectRemovedDaemonArgs([]string{"--saga-store=redis"}), "not supported")
	require.ErrorContains(t, rejectRemovedDaemonArgs([]string{"--plugin-dir", "plugins"}), "not supported")
	require.NoError(t, rejectRemovedDaemonArgs([]string{"--extensions-dir", "extensions"}))
}

func TestVerbDirAliasAcceptsOnlyCanonicalExtensionManifests(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "orders.verbs.json"), []byte(`{}`), 0o600))
	require.NoError(t, validateLegacyVerbDirAlias(directory))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "legacy.json"), []byte(`{}`), 0o600))
	require.ErrorContains(t, validateLegacyVerbDirAlias(directory), "not an extension manifest")
}

func TestVerbHashMismatchFailsClosed(t *testing.T) {
	registry := verb.NewRegistry(nil)
	require.NoError(t, registry.RegisterVerb(&verb.Spec{Name: "record", ArgTypes: map[string]string{}, ReturnType: "void"}))

	require.NoError(t, validateBundleVerbHash(registry.GetVerbHash(), registry))
	require.ErrorContains(t, validateBundleVerbHash("", registry), "missing")
	require.ErrorContains(t, validateBundleVerbHash(registry.GetVerbHash(), nil), "missing")
	require.ErrorContains(t, validateBundleVerbHash("different", registry), "mismatch")
}
