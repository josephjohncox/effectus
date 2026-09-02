package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONVerbLoaderRejectsWorkflowFieldBeforeRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extension.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
  "name":"test",
  "version":"1",
  "verbs":[{"name":"write","argTypes":{},"requiredArgs":[],"returnType":"void","target":{"type":"noop"}}],
  "workflows":[{"name":"workflow","steps":[]}]
}`), 0o600))
	target := &countingSourceTarget{}
	err := NewJSONVerbLoader("test", path).Load(t.Context(), target)
	require.ErrorContains(t, err, `unknown field "workflows"`)
	require.Zero(t, target.verbs)
	require.Empty(t, target.sources)
}

func TestJSONVerbLoaderRejectsDuplicateKeysBeforeRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extension.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
  "name":"test",
  "version":"1",
  "verbs":[],
  "name":"duplicate"
}`), 0o600))
	target := &countingSourceTarget{}
	err := NewJSONVerbLoader("test", path).Load(t.Context(), target)
	require.ErrorContains(t, err, "duplicate JSON object key")
	require.Zero(t, target.verbs)
}

func TestStaticSourceLoaderCopiesEffxBytes(t *testing.T) {
	data := []byte(`flow "one" priority 1 { when {} steps {} }`)
	target := &countingSourceTarget{}
	require.NoError(t, NewStaticSourceLoader("test", "rules/one.effx", data).Load(t.Context(), target))
	require.Len(t, target.sources, 1)
	require.Equal(t, "rules/one.effx", target.sources[0].Path)
	data[0] = 'X'
	require.Equal(t, byte('f'), target.sources[0].Data[0])
}

func TestLoadFromDirectoryDiscoversEffAndEffx(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "rules"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "rules", "one.eff"), []byte(`rule "one" priority 1 {}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "rules", "two.effx"), []byte(`flow "two" priority 1 { when {} steps {} }`), 0o600))
	loaders, err := LoadFromDirectory(directory)
	require.NoError(t, err)
	target := &countingSourceTarget{}
	for _, sourceLoader := range loaders {
		require.NoError(t, sourceLoader.Load(t.Context(), target))
	}
	require.Len(t, target.sources, 2)
	require.Equal(t, []string{"rules/one.eff", "rules/two.effx"}, []string{target.sources[0].Path, target.sources[1].Path})
}

func TestJSONVerbLoaderRejectsOversizedManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extension.json")
	require.NoError(t, os.WriteFile(path, make([]byte, maxExtensionManifestBytes+1), 0o600))
	err := NewJSONVerbLoader("test", path).Load(t.Context(), &countingSourceTarget{})
	require.ErrorContains(t, err, "manifest exceeds")
}

type countingSourceTarget struct {
	verbs   int
	sources []SourceFile
}

func (target *countingSourceTarget) RegisterVerb(VerbSpec, VerbExecutor) error {
	target.verbs++
	return nil
}
func (*countingSourceTarget) RegisterFunction(string, interface{}) error { return nil }
func (*countingSourceTarget) LoadData(string, interface{}) error         { return nil }
func (*countingSourceTarget) RegisterType(string, TypeDefinition) error  { return nil }
func (target *countingSourceTarget) RegisterSource(source SourceFile) error {
	target.sources = append(target.sources, SourceFile{Path: source.Path, Data: append([]byte(nil), source.Data...)})
	return nil
}
