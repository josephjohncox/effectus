package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBufRegistrationIsDeterministicAndNeverRewritesDefinitions(t *testing.T) {
	root := t.TempDir()
	b, err := NewBufIntegration(root)
	require.NoError(t, err)
	schema := &FactSchema{Name: "order", Schema: map[string]any{"z": "integer", "a": "string"}}
	require.NoError(t, b.RegisterFactSchema(t.Context(), schema))
	path := filepath.Join(root, "proto/effectus/v1/facts/order.proto")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(original), "string a = 1;")
	require.Contains(t, string(original), "int64 z = 2;")
	registered, ok := b.GetFactSchema("order")
	require.True(t, ok)
	for i := 0; i < 20; i++ {
		require.NoError(t, b.RegisterFactSchema(t.Context(), schema))
	}
	after, ok := b.GetFactSchema("order")
	require.True(t, ok)
	require.Equal(t, registered.CreatedAt, after.CreatedAt)
	changed := &FactSchema{Name: "order", Schema: map[string]any{"z": "integer", "a": "string", "b": "boolean"}}
	require.ErrorContains(t, b.RegisterFactSchema(t.Context(), changed), "Preserve its field numbers")
	restarted, err := NewBufIntegration(root)
	require.NoError(t, err)
	require.ErrorContains(t, restarted.RegisterFactSchema(t.Context(), changed), "Preserve its field numbers")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, data)
	_, ok = restarted.GetFactSchema("order")
	require.False(t, ok)
	require.NoError(t, restarted.RegisterFactSchema(t.Context(), schema))
	files, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".buf-*.tmp"))
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestBufRegistrationRejectsSymlinkEscapes(t *testing.T) {
	for _, alias := range []string{"directory", "file"} {
		t.Run(alias, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			b, err := NewBufIntegration(root)
			require.NoError(t, err)
			if alias == "directory" {
				require.NoError(t, os.Symlink(outside, filepath.Join(root, "proto")))
			} else {
				directory := filepath.Join(root, "proto/effectus/v1/facts")
				require.NoError(t, os.MkdirAll(directory, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(outside, "original"), []byte("unchanged"), 0644))
				require.NoError(t, os.Symlink(filepath.Join(outside, "original"), filepath.Join(directory, "order.proto")))
			}
			require.Error(t, b.RegisterFactSchema(t.Context(), &FactSchema{Name: "order", Schema: map[string]any{"id": "string"}}))
			entries, err := os.ReadDir(outside)
			require.NoError(t, err)
			if alias == "directory" {
				require.Empty(t, entries)
			} else {
				data, err := os.ReadFile(filepath.Join(outside, "original"))
				require.NoError(t, err)
				require.Equal(t, "unchanged", string(data))
				require.Len(t, entries, 1)
			}
		})
	}
}

func TestBufRegistrationHonorsSingleModulePath(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "buf.yaml"), []byte("version: v2\nmodules:\n  - path: api\n"), 0644))
	b, err := NewBufIntegration(root)
	require.NoError(t, err)
	require.NoError(t, b.RegisterFactSchema(t.Context(), &FactSchema{Name: "order", Schema: map[string]any{"id": "string"}}))
	_, err = os.Stat(filepath.Join(root, "api/effectus/v1/facts/order.proto"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "proto"))
	require.ErrorIs(t, err, os.ErrNotExist)
	for _, config := range []string{"null", "version: v3", "version: v2\nmodules:\n  - path: ../escape\n", "version: v2\nmodules:\n  - path: a\n  - path: b\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "buf.yaml"), []byte(config), 0644))
		_, err := NewBufIntegration(root)
		require.Error(t, err)
	}
}
