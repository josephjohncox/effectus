package runtime

import (
	"context"
	"testing"

	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/ir"
	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema"
	"github.com/stretchr/testify/require"
)

func TestRestartRecoveryLoadsExactArtifactAndResolver(t *testing.T) {
	first := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	ledger := schema.NewInMemoryExecutionLedger()
	outbox := schema.NewInMemoryOutboxStore()
	require.NoError(t, first.ConfigureDurableWorkflowExecution(outbox, nil, schema.DispatcherOptions{Owner: "first"}))
	require.NoError(t, first.ConfigureExecutionLedger(ledger, nil))
	accepted, err := first.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
		ExecutionID: "restart", AdmissionID: "delivery", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"},
	}, WaitMode: WaitAccepted})
	require.NoError(t, err)

	second := NewExecutionRuntime()
	second.EnableLegacyExecutionForCompatibility()
	second.mu.Lock()
	second.compiledUnit, second.state = first.compiledUnit, StateReady
	second.mu.Unlock()
	require.NoError(t, second.ConfigureDurableWorkflowExecution(outbox, nil, schema.DispatcherOptions{Owner: "second"}))
	resolver := ArtifactResolverFunc(func(_ context.Context, artifact schema.ExecutionArtifact, checked *ir.Checked) (*compiler.CompiledUnit, error) {
		require.Equal(t, accepted.GenerationDigest, artifact.GenerationDigest)
		unit := *first.compiledUnit
		unit.CheckedIR = checked
		return &unit, nil
	})
	require.NoError(t, second.ConfigureExecutionLedger(ledger, resolver))
	result, err := second.Engine().Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "restart", WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, accepted.GenerationDigest, result.GenerationDigest)
}

func TestRestartRecoveryBlocksWhenExactResolverMissing(t *testing.T) {
	first := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	ledger := schema.NewInMemoryExecutionLedger()
	outbox := schema.NewInMemoryOutboxStore()
	require.NoError(t, first.ConfigureDurableWorkflowExecution(outbox, nil, schema.DispatcherOptions{Owner: "first"}))
	require.NoError(t, first.ConfigureExecutionLedger(ledger, nil))
	_, err := first.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
		ExecutionID: "missing", AdmissionID: "delivery-missing", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"},
	}, WaitMode: WaitAccepted})
	require.NoError(t, err)

	second := NewExecutionRuntime()
	second.EnableLegacyExecutionForCompatibility()
	require.NoError(t, second.ConfigureDurableWorkflowExecution(outbox, nil, schema.DispatcherOptions{Owner: "second"}))
	require.NoError(t, second.ConfigureExecutionLedger(ledger, nil))
	result, err := second.Engine().Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "missing", WaitMode: WaitTerminal})
	require.ErrorIs(t, err, ErrBlockedDependency)
	require.Equal(t, string(schema.ExecutionBlockedDependency), result.State)
	record, err := ledger.GetExecution(t.Context(), "missing")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionBlockedDependency, record.State)
}
