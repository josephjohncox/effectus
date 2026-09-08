package runtime

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGRPCConstructorValidatesBeforeBinding(t *testing.T) {
	engine, err := NewEngine(lifetimeGeneration(t, "1", recoveryTestExecutor{}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, occupied.Close()) })
	_, err = NewRulesetExecutionServerWithOptions(engine, occupied.Addr().String(), RulesetExecutionServerOptions{})
	require.ErrorContains(t, err, "authenticator")
	require.NotContains(t, err.Error(), "listen for gRPC")
	_, err = NewRulesetExecutionServer(engine, occupied.Addr().String())
	require.ErrorContains(t, err, "NewRulesetExecutionServerWithOptions")
	var nilAuthenticator *BearerTokenAuthenticator
	_, err = NewRulesetExecutionServerWithOptions(engine, occupied.Addr().String(), RulesetExecutionServerOptions{Authenticator: nilAuthenticator, AllowInsecureTransport: true, RulesetName: "language", Version: "1"})
	require.ErrorContains(t, err, "authenticator")
	_, err = NewRulesetExecutionServerWithOptions(engine, occupied.Addr().String(), RulesetExecutionServerOptions{AllowUnauthenticated: true, AllowInsecureTransport: true, RulesetName: "wrong", Version: "1"})
	require.ErrorContains(t, err, "immutable generation")
}

func TestGRPCOnListenerFailureRetainsCallerOwnership(t *testing.T) {
	engine, err := NewEngine(lifetimeGeneration(t, "1", recoveryTestExecutor{}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, err = NewRulesetExecutionServerOnListener(engine, listener, RulesetExecutionServerOptions{})
	require.Error(t, err)
	require.NoError(t, listener.Close(), "failed constructor must not close a caller's listener")
}

func TestGRPCExplicitConstructorDefaults(t *testing.T) {
	engine, err := NewEngine(lifetimeGeneration(t, "1", recoveryTestExecutor{}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	auth, err := NewBearerTokenAuthenticator("test-token")
	require.NoError(t, err)
	server, err := NewRulesetExecutionServerWithOptions(engine, "127.0.0.1:0", RulesetExecutionServerOptions{Authenticator: auth, AllowInsecureTransport: true, RulesetName: "language", Version: "1"})
	require.NoError(t, err)
	defer server.Stop()
	require.Equal(t, defaultGRPCMessageBytes, server.options.MaxReceiveBytes)
	require.Equal(t, defaultGRPCMessageBytes, server.options.MaxSendBytes)
	require.Equal(t, defaultGRPCTimeout, server.options.MaxExecutionDuration)
	require.Equal(t, defaultGRPCConcurrentRPC, server.options.MaxConcurrentRPCs)
}
