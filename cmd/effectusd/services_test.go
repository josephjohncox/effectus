package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/runtime"
	"github.com/stretchr/testify/require"
)

type shutdownConnector struct{ onClose func() }

func (c shutdownConnector) Connect(context.Context) (driver.Conn, error) {
	return shutdownConnection{onClose: c.onClose}, nil
}
func (c shutdownConnector) Driver() driver.Driver { return shutdownDriver{connector: c} }

type shutdownDriver struct{ connector shutdownConnector }

func (d shutdownDriver) Open(string) (driver.Conn, error) {
	return d.connector.Connect(context.Background())
}

type shutdownConnection struct{ onClose func() }

func (c shutdownConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unused test operation")
}
func (c shutdownConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unused test operation")
}
func (c shutdownConnection) Close() error               { c.onClose(); return nil }
func (c shutdownConnection) Ping(context.Context) error { return nil }

func TestHTTPShutdownJoinsHandlersAndWorkersBeforeDependenciesClose(t *testing.T) {
	for _, mode := range []string{"graceful", "deadline", "ignores-cancellation", "service-failure"} {
		t.Run(mode, func(t *testing.T) {
			d, _, closeRunner := newHTTPContractDaemon(t)
			var active, dbClosed atomic.Int64
			var badOrder atomic.Bool
			d.db = sql.OpenDB(shutdownConnector{onClose: func() {
				if active.Load() != 0 || !d.engine.Generation().Closed() {
					badOrder.Store(true)
				}
				dbClosed.Add(1)
			}})
			require.NoError(t, d.db.PingContext(t.Context()))
			started, release, workerStarted, workerStopped, fail := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
			failure := errors.New("injected service failure")
			grace := time.Second
			if mode == "deadline" || mode == "ignores-cancellation" {
				grace = 25 * time.Millisecond
			}
			services, err := prepareDaemonServices(d, daemonServiceConfig{httpAddress: "127.0.0.1:0", token: "token", shutdownGrace: grace, workers: []daemonWorker{
				{"observer", func(ctx context.Context) error {
					close(workerStarted)
					<-ctx.Done()
					close(workerStopped)
					return ctx.Err()
				}},
				{"failing service", func(ctx context.Context) error {
					select {
					case <-fail:
						return failure
					case <-ctx.Done():
						return ctx.Err()
					}
				}},
			}})
			require.NoError(t, err)
			services.http.Handler = d.trackHTTPRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				active.Add(1)
				defer active.Add(-1)
				close(started)
				if mode == "deadline" {
					<-r.Context().Done()
				} else {
					<-release
				}
				if d.engine.Generation().Closed() {
					badOrder.Store(true)
				}
				_, _ = io.WriteString(w, "done")
			}))
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { err := services.run(ctx); err = errors.Join(err, d.close()); done <- err; close(done) }()
			t.Cleanup(func() {
				cancel()
				releaseHandler()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("shutdown did not finish")
				}
				closeRunner()
			})
			requestDone := make(chan error, 1)
			go func() {
				client := &http.Client{Timeout: 5 * time.Second}
				response, err := client.Get("http://" + services.httpListener.Addr().String())
				if err == nil {
					_, err = io.ReadAll(response.Body)
					_ = response.Body.Close()
				}
				requestDone <- err
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not start")
			}
			<-workerStarted
			if mode == "service-failure" {
				close(fail)
			} else {
				cancel()
			}
			<-workerStopped
			require.Eventually(t, func() bool { d.httpMu.Lock(); defer d.httpMu.Unlock(); return d.httpDraining }, time.Second, time.Millisecond)
			response := httptest.NewRecorder()
			d.httpHandler("token").ServeHTTP(response, httptest.NewRequest("GET", "/healthz", nil))
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			if mode != "deadline" {
				if mode == "ignores-cancellation" {
					select {
					case <-requestDone:
					case <-time.After(time.Second):
						t.Fatal("HTTP connection was not canceled at the deadline")
					}
				}
				select {
				case err := <-done:
					t.Fatalf("dependencies closed before the handler exited: %v", err)
				default:
				}
				require.Zero(t, dbClosed.Load())
				require.False(t, d.engine.Generation().Closed())
				releaseHandler()
			}
			var runErr error
			select {
			case runErr = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown did not join handlers")
			}
			switch mode {
			case "graceful":
				require.NoError(t, runErr)
			case "service-failure":
				require.ErrorIs(t, runErr, failure)
			default:
				require.ErrorIs(t, runErr, context.DeadlineExceeded)
			}
			require.Equal(t, int64(1), dbClosed.Load())
			require.False(t, badOrder.Load())
			require.Zero(t, active.Load())
			if mode == "graceful" || mode == "service-failure" {
				require.NoError(t, <-requestDone)
			}
		})
	}
}

func TestPartialServicePreparationFailureClosesOwnedListeners(t *testing.T) {
	d, _, close := newHTTPContractDaemon(t)
	defer close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	failure := errors.New("second listener failed")
	services, err := prepareDaemonServices(d, daemonServiceConfig{httpAddress: "test", grpcAddress: "test", token: "token", listenHTTP: func(string, string) (net.Listener, error) { return listener, nil }, makeGRPC: func(*runtime.Engine, string, runtime.RulesetExecutionServerOptions) (*runtime.RulesetExecutionServer, error) {
		return nil, failure
	}})
	require.Nil(t, services)
	require.ErrorIs(t, err, failure)
	_, err = listener.Accept()
	require.ErrorIs(t, err, net.ErrClosed)
	require.False(t, d.engine.Generation().Closed(), "service preparation must not close its borrowed engine")
}

func TestServiceLimitsAndCanceledStartup(t *testing.T) {
	d, _, close := newHTTPContractDaemon(t)
	defer close()
	_, err := prepareDaemonServices(d, daemonServiceConfig{shutdownGrace: -time.Second, httpAddress: "invalid"})
	require.ErrorContains(t, err, "must not be negative")
	services, err := prepareDaemonServices(d, daemonServiceConfig{httpAddress: "127.0.0.1:0", token: "token"})
	require.NoError(t, err)
	require.Equal(t, defaultHTTPShutdownGrace, services.shutdownGrace)
	require.Equal(t, 30*time.Second, services.http.ReadTimeout)
	require.Equal(t, 35*time.Second, services.http.WriteTimeout)
	require.Equal(t, 60*time.Second, services.http.IdleTimeout)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, services.run(ctx))
	_, err = services.httpListener.Accept()
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestShutdownDoesNotHideWrappedDispositionPersistenceFailures(t *testing.T) {
	for _, failure := range []error{context.DeadlineExceeded, context.Canceled, net.ErrClosed, http.ErrServerClosed} {
		t.Run(failure.Error(), func(t *testing.T) {
			d := &daemon{}
			started := make(chan struct{})
			services := &daemonServices{daemon: d, workers: []daemonWorker{{"recovery", func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				return fmt.Errorf("persist disposition: %w", failure)
			}}}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- services.run(ctx) }()
			<-started
			cancel()
			err := <-done
			require.ErrorIs(t, err, failure)
			require.ErrorContains(t, err, "persist disposition")
		})
	}
}
