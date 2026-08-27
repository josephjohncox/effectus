//go:build integration

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	effectusv1 "github.com/effectus/effectus-go/gen/effectus/v1"
	"github.com/effectus/effectus-go/unified"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestEffectusdGeneratedGRPCServiceUsesDurableEngine(t *testing.T) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		t.Skip("DB_DSN is required")
	}
	sink := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer sink.Close()
	directory := t.TempDir()
	manifest := fmt.Sprintf(`{"name":"test","version":"1","verbs":[{"name":"charge","capabilities":["write"],"resources":[{"resource":"payment","capabilities":["write"]}],"argTypes":{"amount":"int"},"requiredArgs":["amount"],"returnType":"void","target":{"type":"http","config":{"url":%q,"method":"POST","timeout":"2s","allowPrivateNetwork":true}}}]}`, sink.URL)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "extension.verbs.json"), []byte(manifest), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "workflow.effx"), []byte(`flow "charge" priority 1 { when {} steps { charge(amount: 1) } }`), 0o600))

	oldDSN, oldAddr, oldInsecure, oldAuth := *sagaPgDSN, *grpcAddr, *grpcAllowInsecure, *apiAuthMode
	t.Cleanup(func() {
		*sagaPgDSN, *grpcAddr, *grpcAllowInsecure, *apiAuthMode = oldDSN, oldAddr, oldInsecure, oldAuth
	})
	*sagaPgDSN, *grpcAddr, *grpcAllowInsecure, *apiAuthMode = dsn, "127.0.0.1:0", true, "disabled"
	bundle := &unified.Bundle{Name: "orders", Version: "1"}
	execution, db, err := configureDaemonExecutionEngine(t.Context(), bundle, []string{directory}, nil)
	require.NoError(t, err)
	defer execution.Close()
	defer db.Close()
	server, err := configureDaemonGRPCServer(execution, bundle)
	require.NoError(t, err)
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Start() }()
	defer func() { server.Stop(); require.NoError(t, <-serveErrors) }()
	require.Eventually(t, func() bool { return server.Ready() == nil }, time.Second, 10*time.Millisecond)
	connection, err := grpc.NewClient(server.Address().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer connection.Close()
	facts, err := structpb.NewStruct(map[string]any{"id": "42"})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	runID := uuid.NewString()
	response, err := effectusv1.NewRulesetExecutionServiceClient(connection).ExecuteRuleset(ctx, &effectusv1.ExecutionRequest{RulesetName: "orders", Version: "1", Namespace: "tenant-" + runID, IdempotencyKey: "grpc-integration-" + runID, TypedFacts: facts})
	require.NoError(t, err)
	require.True(t, response.Success)
	require.NotEmpty(t, response.Metadata["generation_digest"])
}
