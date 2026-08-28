package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	effectusruntime "github.com/effectus/effectus-go/runtime"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/effectus/effectus-go/unified"
	"github.com/stretchr/testify/require"
)

func TestCheckedEngineReloadRejected(t *testing.T) {
	require.NoError(t, rejectCheckedRuntimeMutation(true, 0, 0))
	require.ErrorContains(t, rejectCheckedRuntimeMutation(false, time.Second, 0), "--reload-interval")
	require.ErrorContains(t, rejectCheckedRuntimeMutation(false, 0, time.Second), "--extensions-reload-interval")
	require.NoError(t, rejectCheckedRuntimeMutation(false, 0, 0))
}

func TestCheckedEngineGenerationActivationRejected(t *testing.T) {
	typeSystem := types.NewTypeSystem()
	registry := verb.NewRegistry(typeSystem)
	state := newServerState(&unified.Bundle{Name: "startup", Version: "1"}, nil, nil, factStoreConfig{}, apiAuth{}, nil, nil, typeSystem, nil, registry, true, nil, false, nil, nil)
	state.SetCheckedEngine(new(effectusruntime.Engine))
	generation := state.generationSnapshot()
	err := state.ActivateGeneration(&unified.Bundle{Name: "candidate", Version: "2"}, typeSystem, registry, generation.id)
	require.ErrorIs(t, err, errCheckedEngineImmutable)
	require.Equal(t, generation.id, state.generationSnapshot().id)
}

func TestDatabasePoolLimitsValidation(t *testing.T) {
	require.NoError(t, validateDatabaseSettings(databaseSettings{MaxOpen: 20, MaxIdle: 5, MaxLifetime: time.Minute, MaxIdleTime: time.Second}))
	require.Error(t, validateDatabaseSettings(databaseSettings{MaxOpen: 0}))
	require.Error(t, validateDatabaseSettings(databaseSettings{MaxOpen: 2, MaxIdle: 3}))
	require.Error(t, validateDatabaseSettings(databaseSettings{MaxOpen: 2, MaxLifetime: -1}))
}

func TestHTTPShutdownUsesConfiguredDeadline(t *testing.T) {
	for _, test := range []struct {
		name    string
		timeout time.Duration
		hold    time.Duration
		wantErr bool
	}{
		{name: "handler finishes within deadline", timeout: time.Second, hold: 25 * time.Millisecond},
		{name: "deadline bounds handler drain", timeout: 20 * time.Millisecond, hold: 200 * time.Millisecond, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				once.Do(func() { close(started) })
				<-release
			})}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			serveDone := make(chan error, 1)
			go func() { serveDone <- server.Serve(listener) }()
			go func() {
				_, _ = http.Get("http://" + listener.Addr().String()) //nolint:gosec,noctx -- local shutdown test
			}()
			<-started
			time.AfterFunc(test.hold, func() { close(release) })
			ctx, cancel := context.WithTimeout(context.Background(), test.timeout)
			defer cancel()
			err = shutdownHTTPServer(ctx, server)
			if test.wantErr {
				require.Error(t, err)
				require.True(t, errors.Is(err, context.DeadlineExceeded))
			} else {
				require.NoError(t, err)
			}
			require.ErrorIs(t, <-serveDone, http.ErrServerClosed)
		})
	}
}
