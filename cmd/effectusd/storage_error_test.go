package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type failingTransportLookupLedger struct {
	schema.ExecutionLedger
	failure         error
	lookups, writes atomic.Int64
}

func (s *failingTransportLookupLedger) GetExecutionByAdmission(context.Context, string) (schema.ExecutionRecord, error) {
	s.lookups.Add(1)
	return schema.ExecutionRecord{}, s.failure
}
func (s *failingTransportLookupLedger) PutArtifact(ctx context.Context, a schema.ExecutionArtifact) error {
	s.writes.Add(1)
	return s.ExecutionLedger.PutArtifact(ctx, a)
}
func (s *failingTransportLookupLedger) AdmitExecution(ctx context.Context, a schema.DurableAdmission) (schema.ExecutionRecord, bool, error) {
	s.writes.Add(1)
	return s.ExecutionLedger.AdmitExecution(ctx, a)
}

func TestStorageFailuresHaveConsistentSanitizedTransportClassification(t *testing.T) {
	for _, test := range []struct {
		name       string
		failure    error
		httpStatus int
		grpcCode   codes.Code
	}{
		{"connection", sql.ErrConnDone, 503, codes.Unavailable},
		{"driver", driver.ErrBadConn, 503, codes.Unavailable},
		{"network", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("secret network failure")}, 503, codes.Unavailable},
		{"canceled-network", &net.OpError{Op: "read", Net: "tcp", Err: context.Canceled}, 408, codes.Canceled},
		{"deadline-network", &net.OpError{Op: "read", Net: "tcp", Err: context.DeadlineExceeded}, 504, codes.DeadlineExceeded},
		{"internal", errors.New("secret internal failure"), 500, codes.Internal},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, calls, closeRunner := newHTTPContractDaemon(t)
			defer closeRunner()
			backing := schema.NewInMemoryExecutionLedger()
			store := &failingTransportLookupLedger{ExecutionLedger: backing, failure: fmt.Errorf("secret ledger detail: %w", test.failure)}
			require.NoError(t, d.engine.ConfigureLedger(store, nil))
			httpResponse := executeHTTPContractRequest(t, d.httpHandler("token"), "token", "lookup-key", `{"namespace":"tenant","facts":{"order.risk":99,"order.id":"one"}}`)
			require.Equal(t, test.httpStatus, httpResponse.Code)
			require.NotContains(t, httpResponse.Body.String(), "secret")
			var body map[string]string
			require.NoError(t, json.Unmarshal(httpResponse.Body.Bytes(), &body))
			require.NotEmpty(t, body["error"])
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			auth, err := runtime.NewBearerTokenAuthenticator("token")
			require.NoError(t, err)
			server, err := runtime.NewRulesetExecutionServerOnListener(d.engine, listener, runtime.RulesetExecutionServerOptions{Authenticator: auth, AllowInsecureTransport: true, RulesetName: "orders", Version: "1"})
			require.NoError(t, err)
			serveDone := make(chan error, 1)
			go func() { serveDone <- server.Start() }()
			connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)
			defer func() { _ = connection.Close(); server.Stop(); require.NoError(t, <-serveDone) }()
			facts, err := structpb.NewStruct(map[string]any{"order.risk": 99, "order.id": "one"})
			require.NoError(t, err)
			ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer token"))
			response, err := effectusv1.NewRulesetExecutionServiceClient(connection).ExecuteRuleset(ctx, &effectusv1.ExecutionRequest{RulesetName: "orders", Version: "1", Namespace: "tenant", IdempotencyKey: "lookup-key", TypedFacts: facts})
			require.Nil(t, response)
			require.Equal(t, test.grpcCode, status.Code(err))
			require.NotContains(t, err.Error(), "secret")
			require.Equal(t, int64(2), store.lookups.Load())
			require.Zero(t, store.writes.Load())
			require.Zero(t, *calls)
			_, err = backing.GetExecutionByAdmission(t.Context(), schema.StableAdmissionID("tenant", "lookup-key", "orders", "1"))
			require.ErrorIs(t, err, schema.ErrExecutionNotFound)
		})
	}
}
