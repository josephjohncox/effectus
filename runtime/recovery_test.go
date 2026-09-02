package runtime

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

type recoveryTestExecutor struct{}

func (recoveryTestExecutor) Invoke(context.Context, invocation.Request) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeSuccess}
}

func TestRecoveryWorkerResumesDurablyAcceptedExecution(t *testing.T) {
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorEmbedded, ResolverID: "test/recovery/v1"})
	require.NoError(t, err)
	source, err := bundle.New(bundle.Spec{Name: "orders", Version: "1", Sources: []bundle.Source{{Path: "orders.eff", Content: `rule "review" priority 1 { when { true } then { Review() } }`}}, Environment: ir.Environment{Verbs: map[string]ir.VerbContract{"Review": {ResultType: "bool"}}}, Executors: map[string]invocation.Descriptor{"Review": descriptor}})
	require.NoError(t, err)
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{ID: "test/recovery/v1", Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
		return recoveryTestExecutor{}, nil, nil
	})}})
	require.NoError(t, err)
	generation, err := CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: source, Resolvers: registry, Production: true})
	require.NoError(t, err)
	engine, err := NewEngine(generation)
	require.NoError(t, err)
	store := schema.NewInMemoryOutboxStore()
	ledger := schema.NewInMemoryExecutionLedger()
	require.NoError(t, engine.ConfigureWorkflow(store, nil, schema.DispatcherOptions{Owner: "recovery"}))
	require.NoError(t, engine.ConfigureLedger(ledger, NewManifestArtifactResolver(registry)))
	t.Cleanup(func() { require.NoError(t, engine.Close()) })

	admission := &Admission{ExecutionID: "recovery-execution", AdmissionID: "recovery-admission", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{}, ExpectedGenerationDigest: engine.ActiveGenerationDigest()}
	accepted, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.True(t, accepted.DurablyAccepted)
	require.False(t, accepted.Completed)

	worker := &RecoveryWorker{Engine: engine, Store: ledger, Owner: "recovery-worker", BatchSize: 1, LeaseDuration: time.Second}
	processed, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	completed, err := ledger.GetExecution(t.Context(), accepted.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionCompleted, completed.State)
}
