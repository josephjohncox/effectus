package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
)

func TestCacheVerifiedSourceBundleWritesCanonicalDocumentAtomically(t *testing.T) {
	source, err := bundle.New(bundle.Spec{
		Name:        "orders",
		Version:     "1",
		Sources:     []bundle.Source{{Path: "rules/orders.eff", Content: "rule Orders {}\n"}},
		Environment: ir.Environment{},
	})
	require.NoError(t, err)
	canonical, err := source.Bytes()
	require.NoError(t, err)
	digest := sha256.Sum256(canonical)
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "source-bundle-sha256-"+fmt.Sprintf("%x", digest)+".json")
	require.NoError(t, os.WriteFile(cachePath, []byte("stale"), 0o600))

	require.NoError(t, cacheVerifiedSourceBundle(cacheDir, source))
	cached, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	require.Equal(t, canonical, cached)

	matches, err := filepath.Glob(filepath.Join(cacheDir, ".effectus-source-bundle-*"))
	require.NoError(t, err)
	require.Empty(t, matches)
}

func TestCacheVerifiedSourceBundleSkipsEmptyCacheDirectory(t *testing.T) {
	source, err := bundle.New(bundle.Spec{Name: "orders", Version: "1", Environment: ir.Environment{}})
	require.NoError(t, err)
	require.NoError(t, cacheVerifiedSourceBundle("", source))
}
