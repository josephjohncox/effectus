package compiler

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/josephjohncox/effectus/loader"
	"github.com/stretchr/testify/require"
)

type networkEffectLoader struct{ calls atomic.Int32 }

func (extension *networkEffectLoader) Name() string { return "network-effect" }
func (extension *networkEffectLoader) Load(_ context.Context, target loader.LoadTarget) error {
	extension.calls.Add(1)
	return target.(loader.SourceLoadTarget).RegisterSource(loader.SourceFile{Path: "empty.effx", Data: []byte(`flow "empty" priority 1 { when {} steps {} }`)})
}

func TestCompileSnapshotDoesNotCallMutableLoaders(t *testing.T) {
	extension := new(networkEffectLoader)
	manager := loader.NewExtensionManager()
	manager.AddLoader(extension)
	snapshot, err := manager.Stage(t.Context(), loader.StageOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(1), extension.calls.Load())
	result, err := NewExtensionCompiler().CompileSnapshot(t.Context(), snapshot)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, int32(1), extension.calls.Load())
}
