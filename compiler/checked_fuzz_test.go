package compiler

import (
	"testing"

	"github.com/effectus/effectus-go/ir"
)

func FuzzCompileChecked(f *testing.F) {
	f.Add([]byte(`rule "empty" priority 1 {}`), ".eff")
	f.Add([]byte(`flow "empty" priority 1 { when {} steps {} }`), ".effx")
	f.Add([]byte(`flow "slot" priority 1 { when {} steps { result = produce() consume(value: $result) } }`), ".effx")
	environment := ir.Environment{Verbs: map[string]ir.VerbContract{
		"produce": {Arguments: map[string]string{}, RequiredArgs: []string{}, ResultType: "string"},
		"consume": {Arguments: map[string]string{"value": "string"}, RequiredArgs: []string{"value"}, ResultType: "void"},
	}}
	f.Fuzz(func(t *testing.T, data []byte, extension string) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		if extension != ".eff" && extension != ".effx" {
			extension = ".eff"
		}
		_, _ = CompileChecked(t.Context(), []Source{{Path: "fuzz" + extension, Data: data}}, environment, CompileOptions{Limits: ir.Limits{MaxArtifactBytes: 128 << 10}})
	})
}
