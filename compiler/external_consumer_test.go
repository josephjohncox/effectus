package compiler_test

import (
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
)

// This test intentionally uses only importable contracts. It ensures that a
// downstream caller can construct one portable source bundle and compile both
// supported language dialects without parser callbacks or compiler internals.
func TestExternalConsumerCompilesSourceBundle(t *testing.T) {
	sourceBundle, err := bundle.New(bundle.Spec{
		Name:    "consumer",
		Version: "v1",
		Sources: []bundle.Source{
			{Path: "rules.eff", Content: `rule "rule" priority 1 {}`},
			{Path: "flow.effx", Content: `flow "flow" priority 1 { when {} steps {} }`},
		},
		Environment: ir.Environment{},
	})
	require.NoError(t, err)

	checked, err := compiler.CompileChecked(t.Context(), sourceBundle, compiler.CompileOptions{})
	require.NoError(t, err)
	require.Len(t, checked.CloneArtifact().Plans, 2)
}
