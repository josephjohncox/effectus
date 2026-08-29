package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/ir"
	"github.com/stretchr/testify/require"
)

func TestSharedFrontEnd(t *testing.T) {
	dir := t.TempDir()
	listPath := filepath.Join(dir, "list.eff")
	flowPath := filepath.Join(dir, "flow.effx")
	listData := []byte(`rule "list" priority 1 { when { ready } then { record(value: "list") } }`)
	flowData := []byte(`flow "flow" priority 1 { when { ready } steps { record(value: "flow") } }`)
	require.NoError(t, os.WriteFile(listPath, listData, 0o600))
	require.NoError(t, os.WriteFile(flowPath, flowData, 0o600))
	environment := ir.Environment{
		Facts: map[string]string{"ready": "bool"},
		Verbs: map[string]ir.VerbContract{"record": {
			Arguments: map[string]string{"value": "string"}, RequiredArgs: []string{"value"}, ResultType: "void",
		}},
	}

	loaded, err := LoadSources([]string{listPath, flowPath})
	require.NoError(t, err)
	fromFiles, err := CompileChecked(t.Context(), loaded, environment, CompileOptions{})
	require.NoError(t, err)
	inspected := make(map[string]int)
	direct, err := CompileChecked(t.Context(), []Source{{Path: listPath, Content: listData}, {Path: flowPath, Content: flowData}}, environment, CompileOptions{InspectSource: func(path string, _ *ast.File) {
		inspected[path]++
	}})
	require.NoError(t, err)
	require.Equal(t, direct.Marshal(), fromFiles.Marshal())
	require.Equal(t, map[string]int{filepath.ToSlash(filepath.Clean(listPath)): 1, filepath.ToSlash(filepath.Clean(flowPath)): 1}, inspected)
}
