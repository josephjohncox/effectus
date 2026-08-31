package runtime

import (
	"context"
	"net"
	"testing"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/loader"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestValidateExecutionOptionsNamesUnsupportedField(t *testing.T) {
	tests := []struct {
		name    string
		options *effectusv1.ExecutionOptions
		message string
	}{
		{"dry run", &effectusv1.ExecutionOptions{DryRun: true}, "options.dry_run is not supported"},
		{"max effects", &effectusv1.ExecutionOptions{MaxEffects: 1}, "options.max_effects is not supported"},
		{"tracing", &effectusv1.ExecutionOptions{EnableTracing: true}, "options.enable_tracing is not supported"},
		{"capabilities", &effectusv1.ExecutionOptions{CapabilityFilter: []string{"write"}}, "options.capability_filter is not supported"},
		{"minimum schema", &effectusv1.ExecutionOptions{MinSchemaVersion: "1"}, "options.min_schema_version is not supported"},
		{"maximum schema", &effectusv1.ExecutionOptions{MaxSchemaVersion: "2"}, "options.max_schema_version is not supported"},
		{"negative timeout", &effectusv1.ExecutionOptions{TimeoutSeconds: -1}, "options.timeout_seconds must not be negative"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.EqualError(t, validateExecutionOptions(test.options), test.message)
		})
	}
	require.NoError(t, validateExecutionOptions(&effectusv1.ExecutionOptions{TimeoutSeconds: 1}))
}

func TestGeneratedGRPCServiceExecutesThroughEngine(t *testing.T) {
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	require.NoError(t, RegisterEngineExecutionServiceWithOptions(server, runtime.Engine(), EngineExecutionServiceOptions{RulesetName: "orders", Version: "1"}))
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		require.NoError(t, listener.Close())
		<-serveErrors
	})
	connection, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	facts, err := structpb.NewStruct(map[string]any{"order_id": "42"})
	require.NoError(t, err)
	response, err := effectusv1.NewRulesetExecutionServiceClient(connection).ExecuteRuleset(t.Context(), &effectusv1.ExecutionRequest{
		RulesetName: "orders", Version: "1", Namespace: "tenant", IdempotencyKey: "request-1", TypedFacts: facts,
		WaitMode: effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_ACCEPTED,
	})
	require.NoError(t, err)
	require.True(t, response.Success)
	require.NotEmpty(t, response.ExecutionId)
	require.Equal(t, "accepted", response.Metadata["state"])
}
