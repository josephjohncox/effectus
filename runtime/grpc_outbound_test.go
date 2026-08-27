package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestGRPCRetryEligibilityIsRestrictedToTransientStatuses(t *testing.T) {
	require.True(t, grpcRetryEligible(t.Context(), status.Error(codes.Unavailable, "unavailable")))
	require.True(t, grpcRetryEligible(t.Context(), status.Error(codes.Aborted, "aborted")))
	require.False(t, grpcRetryEligible(t.Context(), status.Error(codes.InvalidArgument, "invalid")))
	require.False(t, grpcRetryEligible(t.Context(), status.Error(codes.PermissionDenied, "denied")))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.False(t, grpcRetryEligible(ctx, status.Error(codes.Unavailable, "unavailable")))
}

func TestGRPCConnectionFactoryCloseClearsConnections(t *testing.T) {
	connection, err := grpc.NewClient("passthrough:///unused", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	factory := &GRPCExecutorFactory{conns: map[string]*grpc.ClientConn{"connection": connection}}
	require.NoError(t, factory.Close())
	require.Empty(t, factory.conns)
	require.NoError(t, factory.Close())
}

func TestGRPCInvocationErrorRequiresExplicitRetrySafety(t *testing.T) {
	failure := grpcInvocationError{err: errors.New("transient"), retrySafe: false}
	require.False(t, failure.Retryable())
	require.ErrorContains(t, failure, "transient")
}
