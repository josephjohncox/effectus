package runtime

import (
	"net"
	"testing"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestReservedGRPCCapabilityMatrix(t *testing.T) {
	expected := map[string][]string{
		"effectus.v1.RulesetExecutionService": {"ExecuteRuleset", "GetRulesetInfo", "ListRulesets", "RegisterRuleset", "UnregisterRuleset", "StreamExecution", "ValidateSchema", "GetSchemaVersion"},
		"effectus.v1.FactRegistryService":     {"RegisterFactSchema", "GetFactSchema", "ListFactSchemas", "ValidateFactData", "CheckCompatibility"},
		"effectus.v1.VerbRegistryService":     {"RegisterVerbInterface", "GetVerbInterface", "ListVerbInterfaces", "ValidateVerbCall", "CheckInterfaceCompatibility", "GenerateVerbCode"},
	}
	var services []protoreflect.ServiceDescriptor
	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if file.Package() == "effectus.v1" {
			for i := 0; i < file.Services().Len(); i++ {
				services = append(services, file.Services().Get(i))
			}
		}
		return true
	})
	require.Len(t, services, len(expected), "new services require an explicit capability review")
	engine := languageEngine(t, lifetimeGeneration(t, "1", recoveryTestExecutor{}), schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	auth, err := NewBearerTokenAuthenticator("token")
	require.NoError(t, err)
	server, err := NewRulesetExecutionServerOnListener(engine, listener, RulesetExecutionServerOptions{Authenticator: auth, AllowInsecureTransport: true, RulesetName: "language", Version: "1"})
	require.NoError(t, err)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Start() }()
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = connection.Close(); server.Stop(); require.NoError(t, <-serveDone) }()
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer token"))
	for _, service := range services {
		name := string(service.FullName())
		methods, ok := expected[name]
		require.True(t, ok, "unreviewed service %s", name)
		actual := []string{}
		for i := 0; i < service.Methods().Len(); i++ {
			method := service.Methods().Get(i)
			methodName := string(method.Name())
			actual = append(actual, methodName)
			if name == "effectus.v1.RulesetExecutionService" && methodName == "ExecuteRuleset" {
				continue
			}
			t.Run(name+"/"+methodName, func(t *testing.T) {
				serviceDeprecated := service.Options().(*descriptorpb.ServiceOptions).GetDeprecated()
				methodDeprecated := method.Options().(*descriptorpb.MethodOptions).GetDeprecated()
				require.True(t, serviceDeprecated || methodDeprecated, "reserved RPC lacks a deprecation marker")
				var callErr error
				if method.IsStreamingServer() {
					require.Equal(t, "StreamExecution", methodName)
					stream, err := effectusv1.NewRulesetExecutionServiceClient(connection).StreamExecution(ctx, &effectusv1.ExecutionRequest{})
					callErr = err
					if callErr == nil {
						_, callErr = stream.Recv()
					}
				} else {
					callErr = connection.Invoke(ctx, "/"+name+"/"+methodName, &emptypb.Empty{}, &emptypb.Empty{})
				}
				require.Equal(t, codes.Unimplemented, status.Code(callErr))
			})
		}
		require.ElementsMatch(t, methods, actual, "update the documented capability matrix before changing RPCs")
	}
}
