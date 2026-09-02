package compiler

import (
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
)

func TestCompileCheckedConsumesCanonicalSourceBundle(t *testing.T) {
	environment := ir.Environment{
		Facts: map[string]string{"ready": "bool"},
		Verbs: map[string]ir.VerbContract{"record": {
			Arguments: map[string]string{"value": "string"}, RequiredArgs: []string{"value"}, ResultType: "void",
		}},
	}
	first := checkedBundle(t, environment,
		bundle.Source{Path: "list.eff", Content: `rule "list" priority 1 { when { ready } then { record(value: "list") } }`},
		bundle.Source{Path: "flow.effx", Content: `flow "flow" priority 1 { when { ready } steps { record(value: "flow") } }`},
	)
	second := checkedBundle(t, environment,
		bundle.Source{Path: "flow.effx", Content: `flow "flow" priority 1 { when { ready } steps { record(value: "flow") } }`},
		bundle.Source{Path: "list.eff", Content: `rule "list" priority 1 { when { ready } then { record(value: "list") } }`},
	)
	firstChecked, err := CompileChecked(t.Context(), first, CompileOptions{})
	require.NoError(t, err)
	secondChecked, err := CompileChecked(t.Context(), second, CompileOptions{})
	require.NoError(t, err)
	require.Equal(t, firstChecked.Marshal(), secondChecked.Marshal())
}
