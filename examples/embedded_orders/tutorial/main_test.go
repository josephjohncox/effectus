package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/compiler"
	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
)

func TestTutorialDialectsExecuteBindingsAndReplay(t *testing.T) {
	for _, dialect := range []string{"eff", "effx"} {
		t.Run(dialect, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			require.Zero(t, runCLI([]string{"-dialect", dialect}, &stdout, &stderr), stderr.String())
			var result tutorialSummary
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
			require.True(t, result.Completed)
			require.NotEmpty(t, result.ExecutionID)
			require.Equal(t, result.ExecutionID, result.ReplayID)
			require.NotEmpty(t, result.GenerationDigest)
			require.Equal(t, []operation{{Verb: "RequestManualReview", Ticket: "ticket:order-200"}, {Verb: "RecordReview", Ticket: "ticket:order-200"}}, result.Operations)
			stdout.Reset()
			require.Zero(t, runCLI([]string{"-dialect", dialect, "-total", "25", "-risk-score", "10"}, &stdout, &stderr), stderr.String())
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
			require.True(t, result.Completed)
			require.Empty(t, result.Operations)
		})
	}
}

func TestTutorialBundleAndIRRoundTrip(t *testing.T) {
	for _, dialect := range []string{"eff", "effx"} {
		t.Run(dialect, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			require.Zero(t, runCLI([]string{"-dialect", dialect, "-bundle"}, &stdout, &stderr), stderr.String())
			source, err := bundle.Parse(stdout.Bytes())
			require.NoError(t, err)
			checked, err := compiler.CompileChecked(t.Context(), source, compiler.CompileOptions{})
			require.NoError(t, err)
			parsed, err := ir.Parse(checked.Marshal(), source.Environment(), ir.ValidationDefaults())
			require.NoError(t, err)
			require.Equal(t, checked.Digest(), parsed.Digest())
			artifact := checked.CloneArtifact()
			require.Len(t, artifact.Plans, 1)
			plan := artifact.Plans[0]
			if dialect == "effx" {
				require.Equal(t, effectusv1.SourceDialect_SOURCE_DIALECT_FLOW, plan.SourceDialect)
			} else {
				require.Equal(t, effectusv1.SourceDialect_SOURCE_DIALECT_LIST, plan.SourceDialect)
			}
			require.Len(t, plan.Steps, 2)
			// The producer's slot is consumed by the second step, not by a fact lookup.
			require.Equal(t, uint32(0), plan.Steps[0].GetResultSlot())
			for _, argument := range plan.Steps[1].Arguments {
				if argument.Name == "ticket" {
					require.IsType(t, &effectusv1.Value_ResultSlot{}, argument.Value.Kind)
					require.Equal(t, uint32(0), argument.Value.GetResultSlot())
					return
				}
			}
			t.Fatal("consumer has no ticket argument")
		})
	}
}

func TestDocumentedTutorialSnippetsAndCommands(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/BASICS.md")
	require.NoError(t, err)
	for _, dialect := range []string{"eff", "effx"} {
		_, after, found := strings.Cut(string(doc), "```"+dialect+"\n")
		require.True(t, found)
		snippet, _, closed := strings.Cut(after, "```")
		require.True(t, closed)
		source, err := rules.ReadFile("rules/review." + dialect)
		require.NoError(t, err)
		require.Equal(t, string(source), snippet, "the compiled fixture and documentation must agree")
	}
	commands := 0
	for _, line := range strings.Split(string(doc), "\n") {
		args, found := strings.CutPrefix(line, "go run ./examples/embedded_orders/tutorial ")
		if !found {
			continue
		}
		// The documented redirect captures bundle bytes. It is not executed as shell input.
		args, _, _ = strings.Cut(args, " > ")
		t.Run(args, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runCLI(strings.Fields(args), &stdout, &stderr)
			if strings.Contains(args, "-diagnostic=") {
				require.Equal(t, 1, code)
				require.Empty(t, stdout.String())
				require.NotEmpty(t, stderr.String())
			} else {
				require.Zero(t, code, stderr.String())
				require.True(t, json.Valid(stdout.Bytes()))
			}
		})
		commands++
	}
	require.GreaterOrEqual(t, commands, 7, "tutorial must retain runnable execution, bundle, and diagnostic examples")
}

func TestTutorialDiagnosticsAndCLIContract(t *testing.T) {
	for _, dialect := range []string{"eff", "effx"} {
		for _, diagnostic := range []string{"unknown-fact", "type-mismatch", "future-binding"} {
			t.Run(dialect+"/"+diagnostic, func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				require.Equal(t, 1, runCLI([]string{"-dialect", dialect, "-diagnostic", diagnostic}, &stdout, &stderr))
				require.Empty(t, stdout.String())
				require.NotEmpty(t, stderr.String())
			})
		}
	}
	for _, test := range []struct {
		args []string
		code int
	}{{[]string{"-help"}, 0}, {[]string{"-unknown"}, 2}, {[]string{"positional"}, 2}, {[]string{"-dialect", "unknown"}, 1}} {
		var stdout, stderr bytes.Buffer
		require.Equal(t, test.code, runCLI(test.args, &stdout, &stderr))
		require.Empty(t, stdout.String())
		require.NotEmpty(t, stderr.String())
	}
}
