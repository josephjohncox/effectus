package runtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDynamicGRPCConstructorFailsClosed(t *testing.T) {
	server, err := NewRulesetExecutionServer(nil, "127.0.0.1:0")
	require.ErrorIs(t, err, ErrDynamicGRPCUnsupported)
	require.Nil(t, server)
}

func TestDynamicGRPCRegistrationFailsClosed(t *testing.T) {
	server := &RulesetExecutionServer{
		rulesets: make(map[string]*CompiledRuleset),
		services: make(map[string]*RulesetService),
	}
	err := server.RegisterRuleset(&CompiledRuleset{
		Name:       "rules",
		Version:    "1.0.0",
		FactSchema: &Schema{Name: "facts"},
	})
	require.ErrorIs(t, err, ErrDynamicGRPCUnsupported)

	rulesets, err := server.ListRulesets(t.Context())
	require.NoError(t, err)
	require.Empty(t, rulesets)
}

func TestDynamicGRPCExecutionFailsClosed(t *testing.T) {
	service := &RulesetService{ruleset: &CompiledRuleset{Name: "rules", Version: "1.0.0"}}
	response, err := service.Execute(t.Context(), &ExecutionRequest{RulesetName: "rules"})
	require.ErrorIs(t, err, ErrDynamicGRPCUnsupported)
	require.Nil(t, response)
}

func TestDynamicGRPCHotReloadDoesNotStartPlaceholderLoop(t *testing.T) {
	server := &RulesetExecutionServer{}
	server.EnableHotReload(0)
	require.False(t, server.hotReload)
	server.EnableHotReload(time.Second)
	require.False(t, server.hotReload)
	require.Nil(t, server.reloadChecker)
}
