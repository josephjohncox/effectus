package runtime

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/loader"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type blockingGRPCVerb struct{}
type blockingVerbSpec struct{}
type blockingResourceSpec struct{}

func (blockingVerbSpec) GetName() string           { return "charge" }
func (blockingVerbSpec) GetDescription() string    { return "" }
func (blockingVerbSpec) GetCapabilities() []string { return []string{"write"} }
func (blockingVerbSpec) GetResources() []loader.ResourceSpec {
	return []loader.ResourceSpec{blockingResourceSpec{}}
}
func (blockingVerbSpec) GetArgTypes() map[string]string { return map[string]string{"amount": "int"} }
func (blockingVerbSpec) GetRequiredArgs() []string      { return []string{"amount"} }
func (blockingVerbSpec) GetReturnType() string          { return "void" }
func (blockingVerbSpec) GetInverseVerb() string         { return "" }
func (blockingResourceSpec) GetResource() string        { return "payment" }
func (blockingResourceSpec) GetCapabilities() []string  { return []string{"write"} }

func (blockingGRPCVerb) Execute(ctx context.Context, _ map[string]any) (any, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestStableGeneratedGRPCServerAuthenticationPinningAndManagement(t *testing.T) {
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	authenticator, err := NewBearerTokenAuthenticatorSet([]string{"one", "two"})
	require.NoError(t, err)
	server, client := startStableGRPCServer(t, runtime, RulesetExecutionServerOptions{
		Authenticator: authenticator, AllowInsecureTransport: true, RulesetName: "orders", Version: "1",
	})
	_ = server
	facts, err := structpb.NewStruct(map[string]any{"order_id": "42"})
	require.NoError(t, err)
	request := &effectusv1.ExecutionRequest{RulesetName: "orders", Version: "1", Namespace: "tenant", IdempotencyKey: "delivery", TypedFacts: facts, WaitMode: effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_ACCEPTED}
	_, err = client.ExecuteRuleset(t.Context(), request)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer two"))
	response, err := client.ExecuteRuleset(ctx, request)
	require.NoError(t, err)
	require.True(t, response.Success)
	require.NotEmpty(t, response.Metadata["generation_digest"])

	mismatch := proto.Clone(request).(*effectusv1.ExecutionRequest)
	mismatch.GenerationDigest = "sha256:wrong"
	mismatch.IdempotencyKey = "other"
	_, err = client.ExecuteRuleset(ctx, mismatch)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	wrongVersion := proto.Clone(request).(*effectusv1.ExecutionRequest)
	wrongVersion.Version = "2"
	wrongVersion.IdempotencyKey = "version"
	_, err = client.ExecuteRuleset(ctx, wrongVersion)
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = client.GetRulesetInfo(ctx, &effectusv1.RulesetInfoRequest{RulesetName: "orders"})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = client.RegisterRuleset(ctx, &effectusv1.RegisterRulesetRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestStableGRPCServerEnforcesDeadlineAndSanitizesFailure(t *testing.T) {
	runtime := NewExecutionRuntime()
	runtime.EnableLegacyExecutionForCompatibility()
	runtime.RegisterExtensionLoader(loader.NewStaticVerbLoader("blocking", []loader.VerbDefinition{{
		Spec:     blockingVerbSpec{},
		Executor: blockingGRPCVerb{},
	}}))
	runtime.RegisterExtensionLoader(loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	require.NoError(t, runtime.CompileAndValidate(t.Context()))
	require.NoError(t, runtime.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "grpc-test"}))
	_, client := startStableGRPCServer(t, runtime, RulesetExecutionServerOptions{AllowUnauthenticated: true, AllowInsecureTransport: true, RulesetName: "orders", Version: "1", MaxExecutionDuration: 20 * time.Millisecond})
	facts, err := structpb.NewStruct(map[string]any{"id": "42"})
	require.NoError(t, err)
	_, err = client.ExecuteRuleset(t.Context(), &effectusv1.ExecutionRequest{RulesetName: "orders", Version: "1", Namespace: "tenant", IdempotencyKey: "slow", TypedFacts: facts})
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	require.NotContains(t, status.Convert(err).Message(), "context deadline exceeded")
}

func TestStableGRPCServerRequiresExplicitTLSAndAuth(t *testing.T) {
	listener := bufconn.Listen(1024)
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	_, err := NewRulesetExecutionServerOnListener(runtime, listener, RulesetExecutionServerOptions{AllowUnauthenticated: true, RulesetName: "orders", Version: "1"})
	require.ErrorContains(t, err, "TLS")
	_, err = NewRulesetExecutionServerOnListener(runtime, listener, RulesetExecutionServerOptions{AllowInsecureTransport: true, RulesetName: "orders", Version: "1"})
	require.ErrorContains(t, err, "authenticator")
	require.NoError(t, listener.Close())
}

func TestGRPCConcurrentLimitFailsClosed(t *testing.T) {
	admission := make(chan struct{}, 1)
	interceptor := stableExecutionUnaryInterceptor(RulesetExecutionServerOptions{AllowUnauthenticated: true, MaxExecutionDuration: time.Second}, admission)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := interceptor(t.Context(), struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/effectus.v1.RulesetExecutionService/ExecuteRuleset"}, func(context.Context, any) (any, error) { close(entered); <-release; return struct{}{}, nil })
		done <- err
	}()
	<-entered
	_, err := interceptor(t.Context(), struct{}{}, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) { return struct{}{}, nil })
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	close(release)
	require.NoError(t, <-done)
}

func TestGRPCTLSMinimumVersionIsHardened(t *testing.T) {
	options, err := normalizeGRPCOptions(RulesetExecutionServerOptions{AllowUnauthenticated: true, TLSConfig: &tls.Config{Certificates: []tls.Certificate{{}}}, RulesetName: "orders", Version: "1"})
	require.NoError(t, err)
	require.Equal(t, uint16(tls.VersionTLS12), options.TLSConfig.MinVersion)
}

func TestGRPCStatusSanitizerDoesNotExposeInternalErrors(t *testing.T) {
	err := sanitizeGRPCStatus(errors.New("database password secret"))
	require.Equal(t, codes.Internal, status.Code(err))
	require.NotContains(t, status.Convert(err).Message(), "secret")
}

func startStableGRPCServer(t *testing.T, runtime *ExecutionRuntime, options RulesetExecutionServerOptions) (*RulesetExecutionServer, effectusv1.RulesetExecutionServiceClient) {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server, err := NewRulesetExecutionServerOnListener(runtime, listener, options)
	require.NoError(t, err)
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Start() }()
	connection, err := grpc.NewClient("passthrough:///effectus-grpc", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()); server.Stop(); require.NoError(t, <-serveErrors) })
	return server, effectusv1.NewRulesetExecutionServiceClient(connection)
}
