package runtime

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

// A restart reconstructs transport state from the durable artifact; it does
// not depend on a mutable runtime publication or a loader snapshot.
func TestEngineRestartPreservesPinnedGeneration(t *testing.T) {
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: "test/restart/v1"})
	require.NoError(t, err)
	source, err := bundle.New(bundle.Spec{Name: "orders", Version: "1", Sources: []bundle.Source{{Path: "orders.eff", Content: `rule "review" priority 1 { when { true } then { Review() } }`}}, Environment: ir.Environment{Verbs: map[string]ir.VerbContract{"Review": {ResultType: "string"}}}, Executors: map[string]invocation.Descriptor{"Review": descriptor}})
	require.NoError(t, err)
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{ID: "test/restart/v1", Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
		return generationTestExecutor{}, io.NopCloser(strings.NewReader("")), nil
	})}})
	require.NoError(t, err)
	firstGeneration, err := CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: source, Resolvers: registry, Production: true})
	require.NoError(t, err)
	store := schema.NewInMemoryOutboxStore()
	ledger := schema.NewInMemoryExecutionLedger()
	first, err := NewEngine(firstGeneration)
	require.NoError(t, err)
	require.NoError(t, first.ConfigureWorkflow(store, nil, schema.DispatcherOptions{Owner: "first"}))
	require.NoError(t, first.ConfigureLedger(ledger, NewManifestArtifactResolver(registry)))
	admission := Admission{ExecutionID: "execution-1", AdmissionID: "admission-1", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{}, ExpectedGenerationDigest: first.ActiveGenerationDigest()}
	accepted, err := first.Execute(t.Context(), ExecuteRequest{Admission: &admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.Equal(t, first.ActiveGenerationDigest(), accepted.GenerationDigest)
	require.NoError(t, first.Close())

	secondGeneration, err := CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: source, Resolvers: registry, Production: true})
	require.NoError(t, err)
	second, err := NewEngine(secondGeneration)
	require.NoError(t, err)
	require.NoError(t, second.ConfigureWorkflow(store, nil, schema.DispatcherOptions{Owner: "second"}))
	require.NoError(t, second.ConfigureLedger(ledger, NewManifestArtifactResolver(registry)))
	completed, err := second.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: accepted.ExecutionID, WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.True(t, completed.Completed)
	require.Equal(t, accepted.GenerationDigest, completed.GenerationDigest)
	require.NoError(t, second.Close())
}

// Generated gRPC admission reaches the same immutable Engine boundary as a
// direct transport adapter: it produces the same terminal state and pin.
func TestGeneratedGRPCUsesSameGenerationAsDirectEngine(t *testing.T) {
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: "test/transport/v1"})
	require.NoError(t, err)
	source, err := bundle.New(bundle.Spec{Name: "orders", Version: "1", Sources: []bundle.Source{{Path: "orders.eff", Content: `rule "review" priority 1 { when { true } then { Review() } }`}}, Environment: ir.Environment{Verbs: map[string]ir.VerbContract{"Review": {ResultType: "string"}}}, Executors: map[string]invocation.Descriptor{"Review": descriptor}})
	require.NoError(t, err)
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{ID: "test/transport/v1", Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
		return generationTestExecutor{}, io.NopCloser(strings.NewReader("")), nil
	})}})
	require.NoError(t, err)
	generation, err := CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: source, Resolvers: registry, Production: true})
	require.NoError(t, err)
	engine, err := NewEngine(generation)
	require.NoError(t, err)
	store := schema.NewInMemoryOutboxStore()
	require.NoError(t, engine.ConfigureWorkflow(store, nil, schema.DispatcherOptions{Owner: "transport"}))
	direct, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "direct", AdmissionID: "direct", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{}}, WaitMode: WaitTerminal})
	require.NoError(t, err)
	service := &EngineExecutionService{Engine: engine, options: EngineExecutionServiceOptions{RulesetName: "orders", Version: "1"}}
	facts, err := structpb.NewStruct(map[string]any{})
	require.NoError(t, err)
	response, err := service.ExecuteRuleset(t.Context(), &effectusv1.ExecutionRequest{RulesetName: "orders", Version: "1", Namespace: "tenant", IdempotencyKey: "grpc", TypedFacts: facts})
	require.NoError(t, err)
	require.Equal(t, direct.GenerationDigest, response.Metadata["generation_digest"])
	require.Equal(t, "completed", response.Metadata["state"])
	require.NoError(t, engine.Close())
}
