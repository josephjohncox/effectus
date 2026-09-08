package main

import (
	"encoding/json"
	"net"
	"testing"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/runtime"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestHTTPAndGRPCShareLogicalAdmissionIdentity(t *testing.T) {
	d, calls, closeRunner := newHTTPContractDaemon(t)
	defer closeRunner()
	httpResponse := executeHTTPContractRequest(t, d.httpHandler("token"), "token", "same-key", `{"universe":" test ","facts":{"order":{"risk":0,"id":"one"}}}`)
	require.Equal(t, 202, httpResponse.Code, httpResponse.Body.String())
	var accepted runtime.ExecuteResult
	require.NoError(t, json.Unmarshal(httpResponse.Body.Bytes(), &accepted))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	auth, err := runtime.NewBearerTokenAuthenticator("token")
	require.NoError(t, err)
	server, err := runtime.NewRulesetExecutionServerOnListener(d.engine, listener, runtime.RulesetExecutionServerOptions{Authenticator: auth, AllowInsecureTransport: true, RulesetName: "orders", Version: "1"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- server.Start() }()
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { require.NoError(t, connection.Close()); server.Stop(); require.NoError(t, <-done) }()
	facts, err := structpb.NewStruct(map[string]any{"order.risk": 0, "order.id": "one"})
	require.NoError(t, err)
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer token"))
	response, err := effectusv1.NewRulesetExecutionServiceClient(connection).ExecuteRuleset(ctx, &effectusv1.ExecutionRequest{RulesetName: "orders", Version: "1", Namespace: " test ", IdempotencyKey: " same-key ", TypedFacts: facts, WaitMode: effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_ACCEPTED})
	require.NoError(t, err)
	require.Equal(t, accepted.ExecutionID, response.ExecutionId)
	require.Equal(t, accepted.GenerationDigest, response.GenerationDigest)
	require.True(t, response.DurablyAccepted)
	require.Zero(t, *calls)
}
