package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/capability"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/stretchr/testify/assert"
)

type recordingExecutor struct {
	name  string
	calls *[]string
	args  map[string]map[string]interface{}
	fail  bool
}

func (e *recordingExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	*e.calls = append(*e.calls, e.name)
	if e.args != nil {
		e.args[e.name] = args
	}
	if e.fail {
		return nil, errors.New("intentional failure")
	}
	return true, nil
}

func TestFlowSagaCompensation(t *testing.T) {
	flowContent := `
flow "FraudInvestigation" priority 10 {
  when {
    transaction.amount > 1000
  }
  steps {
    FreezeAccount(accountId: customer.id, reason: "investigation")
    NotifyRisk(orderId: transaction.id, channel: "pager")
  }
}
`

	factsData := map[string]interface{}{
		"transaction": map[string]interface{}{
			"id":     "txn-42",
			"amount": 2500.0,
		},
		"customer": map[string]interface{}{
			"id": "cust-9",
		},
	}

	provider := pathutil.NewRegistryFactProviderFromMap(factsData)
	typeSystem := types.NewTypeSystem()
	typeSystem.RegisterFactType("transaction.id", types.NewStringType())
	typeSystem.RegisterFactType("transaction.amount", types.NewFloatType())
	typeSystem.RegisterFactType("customer.id", types.NewStringType())

	typeSystem.RegisterVerbType(
		"FreezeAccount",
		map[string]*types.Type{
			"accountId": types.NewStringType(),
			"reason":    types.NewStringType(),
		},
		types.NewBoolType(),
	)
	typeSystem.RegisterVerbType(
		"NotifyRisk",
		map[string]*types.Type{
			"orderId": types.NewStringType(),
			"channel": types.NewStringType(),
		},
		types.NewBoolType(),
	)

	facts := &simpleFacts{
		data:     factsData,
		provider: provider,
		schema:   &typeSystemSchema{ts: typeSystem},
	}

	comp := compiler.NewCompiler()
	compTS := comp.GetTypeSystem()
	for _, path := range typeSystem.GetAllFactPaths() {
		factType, _ := typeSystem.GetFactType(path)
		compTS.RegisterFactType(path, factType)
	}
	compTS.RegisterVerbType(
		"FreezeAccount",
		map[string]*types.Type{
			"accountId": types.NewStringType(),
			"reason":    types.NewStringType(),
		},
		types.NewBoolType(),
	)
	compTS.RegisterVerbType(
		"NotifyRisk",
		map[string]*types.Type{
			"orderId": types.NewStringType(),
			"channel": types.NewStringType(),
		},
		types.NewBoolType(),
	)

	tmpFile := createTempRuleFile(t, flowContent)
	defer cleanupTempFile(tmpFile)

	parsed, err := comp.ParseAndTypeCheck(tmpFile, facts)
	assert.NoError(t, err)

	flowCompiler := &flow.Compiler{}
	specAny, err := flowCompiler.CompileParsedFile(parsed, tmpFile, facts.Schema())
	assert.NoError(t, err)

	calls := []string{}
	capturedArgs := make(map[string]map[string]interface{})

	registry := verb.NewRegistry(nil)
	assert.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:         "FreezeAccount",
		Description:  "Freezes a customer account",
		Capability:   verb.CapWrite,
		Resources:    verb.ResourceSet{{Resource: "account", Cap: verb.CapWrite}},
		ArgTypes:     map[string]string{"accountId": "string", "reason": "string"},
		RequiredArgs: []string{"accountId", "reason"},
		ReturnType:   "bool",
		Inverse:      "UnfreezeAccount",
		Executor: &recordingExecutor{
			name:  "FreezeAccount",
			calls: &calls,
			args:  capturedArgs,
		},
	}))

	assert.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:         "UnfreezeAccount",
		Description:  "Unfreezes a customer account",
		Capability:   verb.CapWrite,
		Resources:    verb.ResourceSet{{Resource: "account", Cap: verb.CapWrite}},
		ArgTypes:     map[string]string{"accountId": "string", "reason": "string"},
		RequiredArgs: []string{"accountId"},
		ReturnType:   "bool",
		Executor: &recordingExecutor{
			name:  "UnfreezeAccount",
			calls: &calls,
			args:  capturedArgs,
		},
	}))

	assert.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:         "NotifyRisk",
		Description:  "Notifies the risk team",
		Capability:   verb.CapWrite,
		Resources:    verb.ResourceSet{{Resource: "notification", Cap: verb.CapWrite}},
		ArgTypes:     map[string]string{"orderId": "string", "channel": "string"},
		RequiredArgs: []string{"orderId", "channel"},
		ReturnType:   "bool",
		Executor: &recordingExecutor{
			name:  "NotifyRisk",
			calls: &calls,
			args:  capturedArgs,
			fail:  true,
		},
	}))

	spec := specAny.(*flow.Spec)
	spec.VerbRegistry = registry
	spec.SagaEnabled = true
	spec.SagaStore = schema.NewInMemorySagaStore()
	spec.CapSystem = capability.NewCapabilitySystem()

	err = spec.Execute(context.Background(), facts, nil)
	assert.Error(t, err)
	assert.Equal(t, []string{"FreezeAccount", "NotifyRisk", "UnfreezeAccount"}, calls)
	assert.Equal(t, "cust-9", capturedArgs["UnfreezeAccount"]["accountId"])
}
