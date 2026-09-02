package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	effectusruntime "github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema/types"
	"github.com/josephjohncox/effectus/schema/verb"
	"github.com/stretchr/testify/require"
)

type visibleRuleExecutor struct{}

func (visibleRuleExecutor) Invoke(context.Context, invocation.Request) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeSuccess}
}

func TestCheckedGenerationDrivesRulesAndDryRun(t *testing.T) {
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: "test/visible/v1", Reference: "https://executor.invalid/review"})
	require.NoError(t, err)
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{
		ID: "test/visible/v1", Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
			return visibleRuleExecutor{}, nil, nil
		}),
	}})
	require.NoError(t, err)
	source, err := bundle.New(bundle.Spec{
		Name: "orders", Version: "1",
		Sources: []bundle.Source{{Path: "order_review.eff", Content: `rule "ReviewLargeOrder" priority 10 { when { order.total > 1000 || order.risk_score > 75 } then { RequestManualReview(orderId: order.id) } }`}},
		Environment: ir.Environment{
			Facts: map[string]string{"order.id": "string", "order.total": "number", "order.risk_score": "int"},
			Verbs: map[string]ir.VerbContract{"RequestManualReview": {Arguments: map[string]string{"orderId": "string"}, RequiredArgs: []string{"orderId"}, ResultType: "string"}},
		},
		Executors: map[string]invocation.Descriptor{"RequestManualReview": descriptor},
	})
	require.NoError(t, err)
	generation, err := effectusruntime.CompileGeneration(t.Context(), effectusruntime.GenerationBuildConfig{Bundle: source, Resolvers: registry, Production: true})
	require.NoError(t, err)
	runtime := effectusruntime.NewExecutionRuntime()
	require.NoError(t, runtime.PublishGeneration(generation))
	defer runtime.Close()

	state := newServerState(nil, nil, newMemoryFactStore(factStoreConfig{}), factStoreConfig{}, apiAuth{mode: "disabled"}, nil, nil, types.NewTypeSystem(), nil, verb.NewRegistry(nil), false, nil, false, nil, nil)
	state.SetCheckedEngine(runtime.Engine())

	rules := httptest.NewRecorder()
	state.handleRules(rules, httptest.NewRequest(http.MethodGet, "/api/rules", nil))
	require.Equal(t, http.StatusOK, rules.Code)
	require.Contains(t, rules.Body.String(), "ReviewLargeOrder")

	dryRun := httptest.NewRecorder()
	state.handleDryRun(dryRun, httptest.NewRequest(http.MethodPost, "/api/playground/dry-run", strings.NewReader(`{"facts":{"order":{"id":"o-1","total":1500,"risk_score":1}}}`)))
	require.Equal(t, http.StatusOK, dryRun.Code)
	var response dryRunResponse
	require.NoError(t, json.Unmarshal(dryRun.Body.Bytes(), &response))
	require.Len(t, response.Rules, 1)
	require.Equal(t, "ReviewLargeOrder", response.Rules[0].Name)
	require.True(t, response.Rules[0].Matched)
}
