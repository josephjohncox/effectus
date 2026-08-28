package main

import (
	"testing"

	"github.com/effectus/effectus-go/schema/verb"
	"github.com/stretchr/testify/require"
)

func TestValidateBundleVerbHash(t *testing.T) {
	registry := verb.NewRegistry(nil)
	require.NoError(t, registry.RegisterVerb(&verb.Spec{Name: "record", ArgTypes: map[string]string{}, ReturnType: "void"}))

	require.NoError(t, validateBundleVerbHash(registry.GetVerbHash(), registry))
	require.ErrorContains(t, validateBundleVerbHash("", registry), "missing")
	require.ErrorContains(t, validateBundleVerbHash(registry.GetVerbHash(), nil), "missing")
	require.ErrorContains(t, validateBundleVerbHash("different", registry), "mismatch")
}
