package runtime

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGRPCLimitsRejectNegativesBeforeBinding(t *testing.T) {
	engine, err := NewEngine(lifetimeGeneration(t, "1", recoveryTestExecutor{}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()
	for _, test := range []struct {
		name string
		set  func(*RulesetExecutionServerOptions)
	}{
		{"receive", func(o *RulesetExecutionServerOptions) { o.MaxReceiveBytes = -1 }},
		{"send", func(o *RulesetExecutionServerOptions) { o.MaxSendBytes = -1 }},
		{"duration", func(o *RulesetExecutionServerOptions) { o.MaxExecutionDuration = -time.Second }},
		{"concurrency", func(o *RulesetExecutionServerOptions) { o.MaxConcurrentRPCs = -1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := RulesetExecutionServerOptions{AllowUnauthenticated: true, AllowInsecureTransport: true, RulesetName: "language", Version: "1"}
			test.set(&options)
			_, err := NewRulesetExecutionServerWithOptions(engine, occupied.Addr().String(), options)
			require.ErrorContains(t, err, "must not be negative")
			require.NotContains(t, err.Error(), "listen for gRPC")
		})
	}
	options, err := normalizeGRPCOptions(RulesetExecutionServerOptions{AllowUnauthenticated: true, AllowInsecureTransport: true, RulesetName: "language", Version: "1", MaxReceiveBytes: 100, MaxSendBytes: 200, MaxExecutionDuration: time.Second, MaxConcurrentRPCs: 2})
	require.NoError(t, err)
	require.Equal(t, 100, options.MaxReceiveBytes)
	require.Equal(t, 200, options.MaxSendBytes)
	require.Equal(t, time.Second, options.MaxExecutionDuration)
	require.Equal(t, 2, options.MaxConcurrentRPCs)
}
