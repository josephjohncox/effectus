package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestGRPCExecutorLoadsBoundedTypedDescriptors(t *testing.T) {
	name, pkg, syntax := "test.proto", "example", "proto3"
	requestName, responseName, serviceName, methodName := "Request", "Response", "Service", "Call"
	requestType, responseType := ".example.Request", ".example.Response"
	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{Name: &name, Package: &pkg, Syntax: &syntax,
		MessageType: []*descriptorpb.DescriptorProto{{Name: &requestName}, {Name: &responseName}},
		Service:     []*descriptorpb.ServiceDescriptorProto{{Name: &serviceName, Method: []*descriptorpb.MethodDescriptorProto{{Name: &methodName, InputType: &requestType, OutputType: &responseType}}}},
	}}}
	payload, err := proto.Marshal(set)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "descriptor.pb")
	require.NoError(t, os.WriteFile(path, payload, 0o600))
	executor, err := NewGRPCExecutor(map[string]interface{}{"address": "localhost:8080", "method": "/example.Service/Call", "insecure": true, "descriptorSet": path, "requestType": "example.Request", "responseType": "example.Response"})
	require.NoError(t, err)
	require.NotNil(t, executor.requestDescriptor)
	require.NotNil(t, executor.responseDescriptor)
	require.Contains(t, executor.descriptorDigest, "sha256:")
}

func TestGRPCExecutorDefaultsToTLSAndRequiresExplicitPlaintext(t *testing.T) {
	secure, err := NewGRPCExecutor(map[string]interface{}{"address": "example.com:443", "method": "/example.Service/Call"})
	require.NoError(t, err)
	require.True(t, secure.UseTLS)
	require.False(t, secure.Insecure)
	connection, err := grpc.NewClient("passthrough:///unused", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	secure.conn = connection
	require.NoError(t, secure.Close())
	require.Nil(t, secure.conn)
	require.NoError(t, secure.Close())

	_, err = NewGRPCExecutor(map[string]interface{}{"address": "localhost:8080", "method": "/example.Service/Call", "useTLS": false})
	require.ErrorContains(t, err, "insecure: true")
	plaintext, err := NewGRPCExecutor(map[string]interface{}{"address": "localhost:8080", "method": "/example.Service/Call", "insecure": true})
	require.NoError(t, err)
	require.False(t, plaintext.UseTLS)
	require.True(t, plaintext.Insecure)
}
