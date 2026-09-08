package runtime

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type shutdownRPCExecutor struct{ started, canceled, release chan struct{} }

func (e shutdownRPCExecutor) Invoke(ctx context.Context, _ invocation.Request) invocation.Outcome {
	close(e.started)
	<-ctx.Done()
	close(e.canceled)
	<-e.release
	return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: ctx.Err()}
}

func TestGRPCStopCancelsAndJoinsHandlersAndConcurrentStopCalls(t *testing.T) {
	executor := shutdownRPCExecutor{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(executor.release) }) }
	engine := languageEngine(t, lifetimeGeneration(t, "1", executor), schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server, err := NewRulesetExecutionServerOnListener(engine, listener, RulesetExecutionServerOptions{AllowUnauthenticated: true, AllowInsecureTransport: true, RulesetName: "language", Version: "1", MaxExecutionDuration: 25 * time.Millisecond})
	require.NoError(t, err)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Start() }()
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { release(); _ = connection.Close(); server.Stop(); require.NoError(t, <-serveDone) })
	callDone := make(chan error, 1)
	go func() {
		_, err := effectusv1.NewRulesetExecutionServiceClient(connection).ExecuteRuleset(t.Context(), grpcTestRequest())
		callDone <- err
	}()
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}
	firstStopped := make(chan struct{})
	go func() { server.Stop(); close(firstStopped) }()
	require.Eventually(t, func() bool { server.mu.Lock(); defer server.mu.Unlock(); return server.stopped }, time.Second, time.Millisecond)
	secondStopped := make(chan struct{})
	go func() { server.Stop(); close(secondStopped) }()
	select {
	case <-executor.canceled:
	case <-time.After(time.Second):
		t.Fatal("RPC was not canceled")
	}
	require.Never(t, func() bool {
		select {
		case <-firstStopped:
			return true
		case <-secondStopped:
			return true
		default:
			return false
		}
	}, 30*time.Millisecond, time.Millisecond)
	release()
	select {
	case <-firstStopped:
	case <-time.After(time.Second):
		t.Fatal("first Stop did not join")
	}
	select {
	case <-secondStopped:
	case <-time.After(time.Second):
		t.Fatal("second Stop did not join")
	}
	require.Error(t, <-callDone)
}
