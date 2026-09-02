package runtime

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/internal/loader"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func TestManifestArtifactResolverReadsCanonicalDescriptorWrittenAtAdmission(t *testing.T) {
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{
		Type: invocation.DescriptorHTTP, ResolverID: "test/restart/v1", Reference: "https://executor.invalid/review",
	})
	require.NoError(t, err)
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{
		ID: "test/restart/v1", Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
			return generationTestExecutor{}, nil, nil
		}),
	}})
	require.NoError(t, err)
	sourceBundle, err := bundle.New(bundle.Spec{
		Name: "orders", Version: "1",
		Sources: []bundle.Source{{Path: "review.eff", Content: `rule "Review" priority 1 { when { true } then { RequestReview(orderId: order.id) } }`}},
		Environment: ir.Environment{
			Facts: map[string]string{"order.id": "string"},
			Verbs: map[string]ir.VerbContract{"RequestReview": {
				Arguments: map[string]string{"orderId": "string"}, RequiredArgs: []string{"orderId"}, ResultType: "string",
			}},
		},
		Executors: map[string]invocation.Descriptor{"RequestReview": descriptor},
	})
	require.NoError(t, err)
	generation, err := CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: sourceBundle, Resolvers: registry, Production: true})
	require.NoError(t, err)

	ledger := schema.NewInMemoryExecutionLedger()
	outbox := schema.NewInMemoryOutboxStore()
	first := NewExecutionRuntime()
	require.NoError(t, first.PublishGeneration(generation))
	require.NoError(t, first.ConfigureDurableWorkflowExecution(outbox, nil, schema.DispatcherOptions{Owner: "first"}))
	require.NoError(t, first.ConfigureExecutionLedger(ledger, NewManifestArtifactResolver(registry)))
	accepted, err := first.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
		ExecutionID: "canonical-restart", AdmissionID: "canonical-delivery", TenantNamespace: "tenant",
		Ruleset: "orders", Version: "1", Facts: map[string]any{"order.id": "one"},
	}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second := NewExecutionRuntime()
	require.NoError(t, second.ConfigureDurableWorkflowExecution(outbox, nil, schema.DispatcherOptions{Owner: "second"}))
	require.NoError(t, second.ConfigureExecutionLedger(ledger, NewManifestArtifactResolver(registry)))
	result, err := second.Engine().Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "canonical-restart", WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, accepted.GenerationDigest, result.GenerationDigest)
	require.NoError(t, second.Close())
}

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
	second.activeGeneration, second.state = first.activeGeneration, StateReady
	second.mu.Unlock()
	require.NoError(t, second.ConfigureDurableWorkflowExecution(outbox, nil, schema.DispatcherOptions{Owner: "second"}))
	resolver := ArtifactResolverFunc(func(_ context.Context, artifact schema.ExecutionArtifact, checked *ir.Checked) (*compiler.CompiledUnit, error) {
		require.Equal(t, accepted.GenerationDigest, artifact.GenerationDigest)
		unit := *first.activeGeneration.unit
		unit.CheckedIR = checked
		return &unit, nil
	})
	require.NoError(t, second.ConfigureExecutionLedger(ledger, resolver))
	result, err := second.Engine().Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "restart", WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, accepted.GenerationDigest, result.GenerationDigest)
}

type recoveryCloseCounter struct{ closed atomic.Int32 }

func (counter *recoveryCloseCounter) Close() error {
	counter.closed.Add(1)
	return nil
}

func TestRestartRecoveryReleasesResolverOwnedResources(t *testing.T) {
	first := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	ledger := schema.NewInMemoryExecutionLedger()
	outbox := schema.NewInMemoryOutboxStore()
	require.NoError(t, first.ConfigureDurableWorkflowExecution(outbox, nil, schema.DispatcherOptions{Owner: "first"}))
	require.NoError(t, first.ConfigureExecutionLedger(ledger, nil))
	_, err := first.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
		ExecutionID: "owned-resources", AdmissionID: "owned-delivery", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"},
	}, WaitMode: WaitAccepted})
	require.NoError(t, err)

	counter := new(recoveryCloseCounter)
	second := NewExecutionRuntime()
	second.EnableLegacyExecutionForCompatibility()
	require.NoError(t, second.ConfigureDurableWorkflowExecution(outbox, nil, schema.DispatcherOptions{Owner: "second"}))
	resolver := ArtifactResolverFunc(func(_ context.Context, _ schema.ExecutionArtifact, checked *ir.Checked) (*compiler.CompiledUnit, error) {
		unit := *first.activeGeneration.unit
		unit.CheckedIR = checked
		unit.ExtensionSnapshot, err = loader.NewResourceSnapshot(counter)
		require.NoError(t, err)
		unit.ExecutionOwnedSnapshot = true
		return &unit, nil
	})
	require.NoError(t, second.ConfigureExecutionLedger(ledger, resolver))
	_, err = second.Engine().Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "owned-resources", WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.Equal(t, int32(1), counter.closed.Load())
	require.NoError(t, second.Close())
	require.Equal(t, int32(1), counter.closed.Load(), "runtime close must not double-close terminal recovery resources")
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
