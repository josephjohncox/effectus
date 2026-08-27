package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseFileNormalizesArrowBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flow.effx")
	require.NoError(t, os.WriteFile(path, []byte(`flow "demo" priority 1 {
when { true }
steps { Send() -> result }
}`), 0600))

	file, err := NewCompiler().ParseFile(path)
	require.NoError(t, err)
	require.Len(t, file.Flows, 1)
	require.Len(t, file.Flows[0].Steps.Steps, 1)
	step := file.Flows[0].Steps.Steps[0]
	require.Equal(t, "result", step.BindName)
	require.Empty(t, step.Arrow)
}

func TestParseFileRejectsTwoBindingForms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flow.effx")
	require.NoError(t, os.WriteFile(path, []byte(`flow "demo" priority 1 {
when { true }
steps { result = Send() -> other }
}`), 0600))

	_, err := NewCompiler().ParseFile(path)
	require.ErrorContains(t, err, "step cannot use both prefix and arrow bindings")
}
