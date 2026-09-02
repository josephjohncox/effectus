package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/internal/unified"
	effectusruntime "github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema/types"
	"github.com/josephjohncox/effectus/schema/verb"
	"github.com/stretchr/testify/require"
)

func TestCheckedEngineRejectsGenerationMutation(t *testing.T) {
	state := newServerState(&unified.Bundle{Name: "stable"}, nil, nil, factStoreConfig{}, apiAuth{}, nil, nil, types.NewTypeSystem(), nil, verb.NewRegistry(nil), true, nil, false, nil, nil)
	state.SetCheckedEngine(&effectusruntime.Engine{})
	generation := state.generationSnapshot()
	require.ErrorIs(t, state.ActivateBundle(&unified.Bundle{Name: "changed"}, generation.id), errCheckedEngineMutation)
	result := state.evaluateRuleHotload(ruleHotloadRequest{}, true)
	require.True(t, result.Conflict)
	require.False(t, result.Applied)
}

func TestClientKeyTrustedProxyPolicy(t *testing.T) {
	state := &serverState{}
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.RemoteAddr = "203.0.113.9:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.2")
	require.Equal(t, "203.0.113.9", state.clientKey(request), "untrusted peers cannot spoof forwarded addresses")

	prefixes, err := parseTrustedProxyCIDRs("10.0.0.0/8,192.0.2.0/24")
	require.NoError(t, err)
	state.SetTrustedProxies(prefixes)
	request.RemoteAddr = "10.0.0.2:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, 192.0.2.9")
	require.Equal(t, "198.51.100.7", state.clientKey(request))
	request.Header.Set("X-Forwarded-For", "not-an-ip, 192.0.2.9")
	require.Equal(t, "10.0.0.2", state.clientKey(request))
}

func TestRateLimiterBoundedExpiryAndAuthOrdering(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := newRateLimiterWithBounds(60, 2, 2, time.Minute)
	limiter.now = func() time.Time { return now }
	require.True(t, limiter.Allow("b"))
	require.True(t, limiter.Allow("a"))
	now = now.Add(time.Second)
	require.True(t, limiter.Allow("c"))
	require.Len(t, limiter.bucket, 2)
	require.NotContains(t, limiter.bucket, "a", "equal-age eviction is deterministic by key")
	now = now.Add(2 * time.Minute)
	require.True(t, limiter.Allow("d"))
	require.Len(t, limiter.bucket, 1)

	authLimiter := newRateLimiterWithBounds(60, 1, 2, time.Minute)
	state := &serverState{auth: apiAuth{mode: "token", tokens: map[string]apiRole{"valid": roleWrite}}, limiter: authLimiter}
	handler := state.withAPIMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for i := 0; i < 10; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/facts", nil)
		request.RemoteAddr = net.JoinHostPort("203.0.113.9", "1234")
		request.Header.Set("X-Effectus-Token", "invalid")
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	require.Empty(t, authLimiter.bucket)
}

func TestHTTPShutdownUsesSuppliedHandlerDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go server.Serve(listener)
	go http.Get("http://" + listener.Addr().String()) //nolint:errcheck
	<-entered
	go func() { time.Sleep(20 * time.Millisecond); close(release) }()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, shutdownHTTPServer(ctx, server))
}

func TestShutdownTimeoutIsReturned(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-release })}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go server.Serve(listener)
	go http.Get("http://" + listener.Addr().String()) //nolint:errcheck
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, shutdownHTTPServer(ctx, server), context.DeadlineExceeded)
	close(release)
	_ = server.Close()
}
