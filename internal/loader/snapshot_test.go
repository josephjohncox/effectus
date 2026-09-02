package loader

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type snapshotTestLoader struct {
	source []byte
	closed *atomic.Int32
}
type snapshotTestExecutor struct{ closed *atomic.Int32 }

func (executor *snapshotTestExecutor) Execute(context.Context, map[string]any) (any, error) {
	return true, nil
}
func (executor *snapshotTestExecutor) Close() error { executor.closed.Add(1); return nil }
func (loader *snapshotTestLoader) Name() string     { return "snapshot-test" }
func (loader *snapshotTestLoader) Load(_ context.Context, target LoadTarget) error {
	if err := target.RegisterVerb(snapshotTestVerbSpec{}, &snapshotTestExecutor{closed: loader.closed}); err != nil {
		return err
	}
	return target.(SourceLoadTarget).RegisterSource(SourceFile{Path: "workflow.effx", Data: append([]byte(nil), loader.source...)})
}

type snapshotTestVerbSpec struct{}

func (snapshotTestVerbSpec) GetName() string                { return "noop" }
func (snapshotTestVerbSpec) GetDescription() string         { return "" }
func (snapshotTestVerbSpec) GetCapabilities() []string      { return []string{"read"} }
func (snapshotTestVerbSpec) GetResources() []ResourceSpec   { return nil }
func (snapshotTestVerbSpec) GetArgTypes() map[string]string { return map[string]string{} }
func (snapshotTestVerbSpec) GetRequiredArgs() []string      { return nil }
func (snapshotTestVerbSpec) GetReturnType() string          { return "void" }
func (snapshotTestVerbSpec) GetInverseVerb() string         { return "" }

type sourceCapture struct{ sources []SourceFile }

func (target *sourceCapture) RegisterVerb(VerbSpec, VerbExecutor) error { return nil }
func (target *sourceCapture) RegisterFunction(string, any) error        { return nil }
func (target *sourceCapture) LoadData(string, any) error                { return nil }
func (target *sourceCapture) RegisterType(string, TypeDefinition) error { return nil }
func (target *sourceCapture) RegisterSource(source SourceFile) error {
	target.sources = append(target.sources, source)
	return nil
}

func TestExtensionSnapshotIsImmutableAndRetiresAfterLastHandle(t *testing.T) {
	closed := new(atomic.Int32)
	mutable := &snapshotTestLoader{source: []byte(`flow "one" priority 1 { when {} steps {} }`), closed: closed}
	manager := NewExtensionManager()
	manager.AddLoader(mutable)
	snapshot, err := manager.Stage(t.Context(), StageOptions{})
	require.NoError(t, err)
	mutable.source = []byte(`flow "changed" priority 1 { when {} steps {} }`)
	target := new(sourceCapture)
	require.NoError(t, snapshot.Load(t.Context(), target))
	require.Contains(t, string(target.sources[0].Data), `"one"`)
	handle, err := snapshot.Acquire()
	require.NoError(t, err)
	require.NoError(t, snapshot.Retire())
	require.Zero(t, closed.Load())
	require.NotNil(t, handle.Snapshot())
	require.NoError(t, handle.Release())
	require.Equal(t, int32(1), closed.Load())
	require.True(t, snapshot.Closed())
}

func TestExtensionStageBoundsAndCleansCandidate(t *testing.T) {
	closed := new(atomic.Int32)
	manager := NewExtensionManager()
	manager.AddLoader(&snapshotTestLoader{source: make([]byte, 32), closed: closed})
	_, err := manager.Stage(t.Context(), StageOptions{MaxSourceBytes: 16})
	require.ErrorContains(t, err, "exceeds")
	require.Equal(t, int32(1), closed.Load())
}
