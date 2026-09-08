package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBufRejectsUnsafeNamesBeforeFilesystemWrites(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	integration, err := NewBufIntegration(root)
	require.NoError(t, err)
	for _, name := range []string{"", "../escape", "a/b", "a\\b", "name\nmessage Other {}", "1name", "_", "_1"} {
		require.Error(t, integration.RegisterVerbSchema(t.Context(), &VerbSchema{Name: name}))
		require.Error(t, integration.RegisterFactSchema(t.Context(), &FactSchema{Name: name}))
	}
	for _, fields := range []map[string]any{{"bad;field": "string"}, {"x\n}": "integer"}} {
		require.Error(t, integration.RegisterVerbSchema(t.Context(), &VerbSchema{Name: "notify", InputSchema: fields}))
		require.Error(t, integration.RegisterVerbSchema(t.Context(), &VerbSchema{Name: "notify", OutputSchema: fields}))
		require.Error(t, integration.RegisterFactSchema(t.Context(), &FactSchema{Name: "order", Schema: fields}))
	}
	_, err = os.Stat(root)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Empty(t, integration.ListVerbSchemas())
	require.Empty(t, integration.ListFactSchemas())
	require.NoError(t, checkBufSchemaNames("notify_v2", map[string]any{"field_1": "string"}))
}
