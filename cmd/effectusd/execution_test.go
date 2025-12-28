package main

import (
	"context"
	"testing"

	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/effectus/effectus-go/unified"
	"github.com/stretchr/testify/require"
)

type captureExecutor struct {
	called bool
	args   map[string]interface{}
}

func (c *captureExecutor) Execute(_ context.Context, args map[string]interface{}) (interface{}, error) {
	c.called = true
	c.args = args
	return true, nil
}

func TestExecuteFactsInvokesVerb(t *testing.T) {
	ruleContent := `
rule "FlagLargeOrder" priority 10 {
	when {
		order.total > 500 && customer.vip == false
	}
	then {
		FlagReview(orderId: order.id, reason: "large order")
	}
}
`
	typeSystem := types.NewTypeSystem()
	typeSystem.RegisterFactType("order.id", types.NewStringType())
	typeSystem.RegisterFactType("order.total", types.NewFloatType())
	typeSystem.RegisterFactType("customer.vip", types.NewBoolType())
	require.NoError(t, typeSystem.RegisterVerbType(
		"FlagReview",
		map[string]*types.Type{
			"orderId": types.NewStringType(),
			"reason":  types.NewStringType(),
		},
		types.NewBoolType(),
	))

	verbReg := verb.NewRegistry(typeSystem)
	exec := &captureExecutor{}
	spec := verb.NewSpec("FlagReview", verb.CapWrite, map[string]string{
		"orderId": "string",
		"reason":  "string",
	}, "bool").WithExecutor(exec)
	require.NoError(t, verbReg.RegisterVerb(spec))

	bundle := &unified.Bundle{
		Name:    "demo",
		Version: "1.0.0",
		RuleSources: []unified.RuleSource{
			{Path: "rules/demo.eff", Format: "eff", Content: ruleContent},
		},
	}

	prepared, err := compileBundleRules(bundle, typeSystem, verbReg, false)
	require.NoError(t, err)
	require.NotNil(t, prepared.ListSpec)

	state := newServerState(prepared, nil, nil, factStoreConfig{}, apiAuth{mode: "disabled"}, nil, nil, typeSystem, nil, verbReg, false, nil, false, nil, nil)

	facts := map[string]interface{}{
		"order": map[string]interface{}{
			"id":    "ORDER-1",
			"total": 750.0,
		},
		"customer": map[string]interface{}{
			"vip": false,
		},
	}

	err = state.ExecuteFacts(context.Background(), factEnvelope{Universe: "default", Facts: facts})
	require.NoError(t, err)
	require.True(t, exec.called)
	require.Equal(t, "ORDER-1", exec.args["orderId"])
	require.Equal(t, "large order", exec.args["reason"])
}
