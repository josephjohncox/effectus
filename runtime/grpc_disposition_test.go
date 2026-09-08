package runtime

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type grpcOutcomeExecutor struct {
	calls   atomic.Int64
	outcome invocation.Outcome
}

func (e *grpcOutcomeExecutor) Invoke(context.Context, invocation.Request) invocation.Outcome {
	e.calls.Add(1)
	return e.outcome
}

func grpcTestClient(t *testing.T, engine *Engine) (effectusv1.RulesetExecutionServiceClient, context.Context) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	auth, err := NewBearerTokenAuthenticator("test-token")
	require.NoError(t, err)
	server, err := NewRulesetExecutionServerOnListener(engine, listener, RulesetExecutionServerOptions{RulesetName: engine.Generation().Ruleset(), Version: engine.Generation().Version(), Authenticator: auth, AllowInsecureTransport: true})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- server.Start() }()
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()); server.Stop(); require.NoError(t, <-done) })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return effectusv1.NewRulesetExecutionServiceClient(connection), metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer test-token"))
}

func grpcTestRequest() *effectusv1.ExecutionRequest {
	return &effectusv1.ExecutionRequest{RulesetName: "language", Version: "1", Namespace: "tenant", IdempotencyKey: "key", TypedFacts: &structpb.Struct{Fields: map[string]*structpb.Value{}}, WaitMode: effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_TERMINAL}
}

func executionDetail(t *testing.T, err error) *effectusv1.ExecutionResponse {
	t.Helper()
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.NotContains(t, err.Error(), "secret")
	details := status.Convert(err).Details()
	require.Len(t, details, 1)
	response, ok := details[0].(*effectusv1.ExecutionResponse)
	require.True(t, ok)
	require.True(t, response.DurablyAccepted)
	require.False(t, response.Success)
	require.False(t, response.Completed)
	require.NotEmpty(t, response.ExecutionId)
	require.NotEmpty(t, response.GenerationDigest)
	return response
}

func TestGRPCWaitModesAndTerminalDispositionsSurviveTheWire(t *testing.T) {
	for _, test := range []struct {
		name    string
		outcome invocation.Outcome
		state   effectusv1.ExecutionState
	}{
		{"completed", invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}, effectusv1.ExecutionState_EXECUTION_STATE_COMPLETED},
		{"failed", invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("secret failure")}, effectusv1.ExecutionState_EXECUTION_STATE_FAILED},
		{"unknown", invocation.Outcome{Class: invocation.OutcomeUnknown, Err: errors.New("secret outcome")}, effectusv1.ExecutionState_EXECUTION_STATE_BLOCKED_UNKNOWN},
		{"fence", invocation.Outcome{Class: invocation.OutcomeStaleFence, Err: errors.New("secret fence")}, effectusv1.ExecutionState_EXECUTION_STATE_BLOCKED_FENCE},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &grpcOutcomeExecutor{outcome: test.outcome}
			engine := languageEngine(t, lifetimeGeneration(t, "1", executor), schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger())
			client, ctx := grpcTestClient(t, engine)
			request := grpcTestRequest()
			request.WaitMode = effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_ACCEPTED
			accepted, err := client.ExecuteRuleset(ctx, request)
			require.NoError(t, err)
			require.True(t, accepted.DurablyAccepted)
			require.False(t, accepted.Success)
			require.False(t, accepted.Completed)
			require.Equal(t, effectusv1.ExecutionState_EXECUTION_STATE_ACCEPTED, accepted.State)
			require.Zero(t, executor.calls.Load())
			request.WaitMode = effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_TERMINAL
			for range 2 {
				response, err := client.ExecuteRuleset(ctx, request)
				if test.state == effectusv1.ExecutionState_EXECUTION_STATE_COMPLETED {
					require.NoError(t, err)
					require.True(t, response.Success)
					require.True(t, response.Completed)
				} else {
					require.Nil(t, response)
					response = executionDetail(t, err)
				}
				require.Equal(t, test.state, response.State)
				require.Equal(t, accepted.ExecutionId, response.ExecutionId)
				require.Equal(t, accepted.GenerationDigest, response.GenerationDigest)
			}
			require.Equal(t, int64(1), executor.calls.Load())
			request.WaitMode = effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_ACCEPTED
			replay, err := client.ExecuteRuleset(ctx, request)
			require.NoError(t, err)
			require.True(t, replay.DurablyAccepted)
			require.Equal(t, test.state, replay.State)
			require.Equal(t, test.state == effectusv1.ExecutionState_EXECUTION_STATE_COMPLETED, replay.Success)
		})
	}
}

func TestGRPCAllBlockedReplayStatesAreTyped(t *testing.T) {
	for _, state := range []schema.ExecutionState{schema.ExecutionBlockedDependency, schema.ExecutionBlockedCompensation} {
		t.Run(string(state), func(t *testing.T) {
			executor := &grpcOutcomeExecutor{outcome: invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}}
			store := schema.NewInMemoryExecutionLedger()
			engine := languageEngine(t, lifetimeGeneration(t, "1", executor), schema.NewInMemoryOutboxStore(), store)
			client, ctx := grpcTestClient(t, engine)
			request := grpcTestRequest()
			request.WaitMode = effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_ACCEPTED
			accepted, err := client.ExecuteRuleset(ctx, request)
			require.NoError(t, err)
			record, err := store.GetExecution(ctx, accepted.ExecutionId)
			require.NoError(t, err)
			_, err = store.SetExecutionState(ctx, record.ExecutionID, record.Revision, state, "secret persisted cause")
			require.NoError(t, err)
			request.WaitMode = effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_TERMINAL
			_, err = client.ExecuteRuleset(ctx, request)
			detail := executionDetail(t, err)
			require.Equal(t, grpcExecutionState(string(state)), detail.State)
			require.Zero(t, executor.calls.Load())
		})
	}
}

func TestGRPCNamespaceAndAuthentication(t *testing.T) {
	executor := &grpcOutcomeExecutor{outcome: invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}}
	engine := languageEngine(t, lifetimeGeneration(t, "1", executor), schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger())
	client, ctx := grpcTestClient(t, engine)
	request := grpcTestRequest()
	request.WaitMode = effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_ACCEPTED
	_, err := client.ExecuteRuleset(t.Context(), request)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	for _, namespace := range []string{"", " \t "} {
		candidate := proto.Clone(request).(*effectusv1.ExecutionRequest)
		candidate.Namespace = namespace
		_, err := client.ExecuteRuleset(ctx, candidate)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
	first, err := client.ExecuteRuleset(ctx, request)
	require.NoError(t, err)
	request.Namespace = " tenant "
	request.IdempotencyKey = " key "
	replay, err := client.ExecuteRuleset(ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.ExecutionId, replay.ExecutionId)
	require.Equal(t, schema.StableExecutionID("tenant", "key", "language", "1"), replay.ExecutionId)
	require.Zero(t, executor.calls.Load())
}
