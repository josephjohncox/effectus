package loader

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/effectus/effectus-go/invocation"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

type metadataService interface {
	Call(context.Context, *structpb.Struct) (*structpb.Struct, error)
}
type metadataServiceImpl struct{ headers chan metadata.MD }

func (service *metadataServiceImpl) Call(ctx context.Context, _ *structpb.Struct) (*structpb.Struct, error) {
	headers, _ := metadata.FromIncomingContext(ctx)
	service.headers <- headers
	return structpb.NewStruct(map[string]any{"ok": true})
}
func metadataCallHandler(server any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	request := new(structpb.Struct)
	if err := decode(request); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return server.(metadataService).Call(ctx, request)
	}
	info := &grpc.UnaryServerInfo{Server: server, FullMethod: "/test.Metadata/Call"}
	return interceptor(ctx, request, info, func(ctx context.Context, request any) (any, error) {
		return server.(metadataService).Call(ctx, request.(*structpb.Struct))
	})
}

func TestKafkaAdapterBuildsInvocationAndFencingHeaders(t *testing.T) {
	request := invocation.Request{Metadata: invocation.Context{ExecutionID: "execution", Saga: invocation.Saga{EffectID: "effect", Attempt: 4, IdempotencyKey: "key"}, FencingGrants: []invocation.FencingGrant{{Authority: "sink", Resource: "account", Token: 11}}}, ArgumentHash: "arguments", ContractHash: "contract"}
	headers := kafkaInvocationHeaders(request)
	values := map[string]string{}
	for _, header := range headers {
		values[header.Key] = string(header.Value)
	}
	require.Equal(t, "execution", values["X-Effectus-Execution-ID"])
	require.Equal(t, "4", values["X-Effectus-Attempt"])
	require.Contains(t, values["X-Effectus-Fencing-Grants"], `"token":11`)
}

func TestGRPCExecutorPropagatesInvocationAndFencingMetadata(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	implementation := &metadataServiceImpl{headers: make(chan metadata.MD, 1)}
	server.RegisterService(&grpc.ServiceDesc{ServiceName: "test.Metadata", HandlerType: (*metadataService)(nil), Methods: []grpc.MethodDesc{{MethodName: "Call", Handler: metadataCallHandler}}}, implementation)
	go server.Serve(listener)
	defer server.Stop()
	executor, err := NewGRPCExecutor(map[string]any{"address": listener.Addr().String(), "method": "/test.Metadata/Call", "insecure": true, "timeout": "2s"})
	require.NoError(t, err)
	defer executor.Close()
	outcome := executor.Invoke(t.Context(), invocation.Request{Metadata: invocation.Context{ExecutionID: "execution", Deadline: time.Now().Add(time.Second), Saga: invocation.Saga{SagaID: "saga", EffectID: "effect", Attempt: 3, Direction: invocation.DirectionForward, IdempotencyKey: "key"}, FencingGrants: []invocation.FencingGrant{{Authority: "sink", Resource: "account", Token: 9}}}, Arguments: map[string]any{"id": "42"}, ArgumentHash: "arguments", ContractHash: "contract"})
	require.Equal(t, invocation.OutcomeSuccess, outcome.Class)
	headers := <-implementation.headers
	require.Equal(t, []string{"execution"}, headers.Get("x-effectus-execution-id"))
	require.Equal(t, []string{"3"}, headers.Get("x-effectus-attempt"))
	require.Contains(t, headers.Get("x-effectus-fencing-grants")[0], `"token":9`)
}
