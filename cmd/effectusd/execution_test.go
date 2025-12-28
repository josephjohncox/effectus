package main

import (
	"context"
	"errors"
	"testing"

	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/capability"
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

type sequenceExecutor struct {
	name  string
	calls *[]string
	fail  bool
}

func (s *sequenceExecutor) Execute(_ context.Context, _ map[string]interface{}) (interface{}, error) {
	if s.calls != nil {
		*s.calls = append(*s.calls, s.name)
	}
	if s.fail {
		return nil, errors.New("intentional failure")
	}
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

func TestExecuteFactsSagaCompensation(t *testing.T) {
	flowContent := `
flow "ChargeOrder" priority 1 {
  when {
    order.total > 100
  }
  steps {
    ReserveInventory(orderId: order.id)
    ChargeCard(orderId: order.id, amount: order.total)
  }
}
`

	typeSystem := types.NewTypeSystem()
	typeSystem.RegisterFactType("order.id", types.NewStringType())
	typeSystem.RegisterFactType("order.total", types.NewFloatType())
	require.NoError(t, typeSystem.RegisterVerbType(
		"ReserveInventory",
		map[string]*types.Type{
			"orderId": types.NewStringType(),
		},
		types.NewBoolType(),
	))
	require.NoError(t, typeSystem.RegisterVerbType(
		"ReleaseInventory",
		map[string]*types.Type{
			"orderId": types.NewStringType(),
		},
		types.NewBoolType(),
	))
	require.NoError(t, typeSystem.RegisterVerbType(
		"ChargeCard",
		map[string]*types.Type{
			"orderId": types.NewStringType(),
			"amount":  types.NewFloatType(),
		},
		types.NewBoolType(),
	))

	calls := []string{}
	verbReg := verb.NewRegistry(typeSystem)
	require.NoError(t, verbReg.RegisterVerb(verb.NewSpec("ReserveInventory", verb.CapWrite, map[string]string{
		"orderId": "string",
	}, "bool").WithInverse("ReleaseInventory").WithExecutor(&sequenceExecutor{name: "ReserveInventory", calls: &calls})))
	require.NoError(t, verbReg.RegisterVerb(verb.NewSpec("ReleaseInventory", verb.CapWrite, map[string]string{
		"orderId": "string",
	}, "bool").WithExecutor(&sequenceExecutor{name: "ReleaseInventory", calls: &calls})))
	require.NoError(t, verbReg.RegisterVerb(verb.NewSpec("ChargeCard", verb.CapWrite, map[string]string{
		"orderId": "string",
		"amount":  "float",
	}, "bool").WithExecutor(&sequenceExecutor{name: "ChargeCard", calls: &calls, fail: true})))

	bundle := &unified.Bundle{
		Name:    "saga-demo",
		Version: "1.0.0",
		RuleSources: []unified.RuleSource{
			{Path: "rules/charge.effx", Format: "effx", Content: flowContent},
		},
	}

	prepared, err := compileBundleRules(bundle, typeSystem, verbReg, false)
	require.NoError(t, err)
	require.NotNil(t, prepared.FlowSpec)

	state := newServerState(
		prepared,
		nil,
		nil,
		factStoreConfig{},
		apiAuth{mode: "disabled"},
		nil,
		nil,
		typeSystem,
		nil,
		verbReg,
		false,
		nil,
		true,
		schema.NewInMemorySagaStore(),
		capability.NewCapabilitySystem(),
	)

	facts := map[string]interface{}{
		"order": map[string]interface{}{
			"id":    "ORD-9",
			"total": 250.0,
		},
	}

	err = state.ExecuteFacts(context.Background(), factEnvelope{Universe: "default", Facts: facts})
	require.Error(t, err)
	require.Equal(t, []string{"ReserveInventory", "ChargeCard", "ReleaseInventory"}, calls)
}
