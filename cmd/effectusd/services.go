package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/josephjohncox/effectus/runtime"
	"google.golang.org/grpc"
)

const defaultHTTPShutdownGrace = 30 * time.Second

type daemonServiceEvent struct {
	name string
	err  error
}

func (event daemonServiceEvent) failure() error {
	if event.err == nil {
		event.err = errors.New("service stopped")
	}
	return fmt.Errorf("%s: %w", event.name, event.err)
}
func normalServiceStop(err error) bool {
	if err == nil || err == context.Canceled || err == context.DeadlineExceeded || err == http.ErrServerClosed || err == net.ErrClosed || err == grpc.ErrServerStopped {
		return true
	}
	// A wrapped operation failure (for example a timed-out durable write) is
	// not silently discarded merely because cancellation is in its error chain.
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !normalServiceStop(child) {
				return false
			}
		}
		return true
	}
	return false
}

type daemonWorker struct {
	name string
	run  func(context.Context) error
}
type daemonServiceConfig struct {
	httpAddress, grpcAddress, token string
	grpcOptions                     runtime.RulesetExecutionServerOptions
	shutdownGrace                   time.Duration
	workers                         []daemonWorker
	listenHTTP                      func(string, string) (net.Listener, error)
	makeGRPC                        func(*runtime.Engine, string, runtime.RulesetExecutionServerOptions) (*runtime.RulesetExecutionServer, error)
}
type daemonServices struct {
	daemon        *daemon
	http          *http.Server
	httpListener  net.Listener
	grpc          *runtime.RulesetExecutionServer
	workers       []daemonWorker
	shutdownGrace time.Duration
}

func prepareDaemonServices(d *daemon, config daemonServiceConfig) (services *daemonServices, err error) {
	if config.shutdownGrace < 0 {
		return nil, fmt.Errorf("HTTP shutdown grace must not be negative")
	}
	if config.shutdownGrace == 0 {
		config.shutdownGrace = defaultHTTPShutdownGrace
	}
	if config.listenHTTP == nil {
		config.listenHTTP = net.Listen
	}
	if config.makeGRPC == nil {
		config.makeGRPC = runtime.NewRulesetExecutionServerWithOptions
	}
	candidate := &daemonServices{daemon: d, workers: config.workers, shutdownGrace: config.shutdownGrace}
	defer func() {
		if err != nil {
			if candidate.httpListener != nil {
				err = errors.Join(err, candidate.httpListener.Close())
			}
			if candidate.grpc != nil {
				candidate.grpc.Stop()
			}
		}
	}()
	if config.httpAddress != "" {
		candidate.httpListener, err = config.listenHTTP("tcp", config.httpAddress)
		if err != nil {
			return nil, fmt.Errorf("listen for HTTP: %w", err)
		}
		candidate.http = &http.Server{Handler: d.httpHandler(config.token), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	}
	if config.grpcAddress != "" {
		candidate.grpc, err = config.makeGRPC(d.engine, config.grpcAddress, config.grpcOptions)
		if err != nil {
			return nil, err
		}
	}
	return candidate, nil
}

// trackHTTPRequests prevents new handlers from racing dependency closure. The
// gate lock makes the transition to Wait safe even when no handler is active.
func (d *daemon) trackHTTPRequests(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.httpMu.Lock()
		if d.httpDraining {
			d.httpMu.Unlock()
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server is draining"})
			return
		}
		d.httpHandlers.Add(1)
		d.httpMu.Unlock()
		defer d.httpHandlers.Done()
		handler.ServeHTTP(w, r)
	})
}

func (d *daemon) stopHTTPAdmission() { d.httpMu.Lock(); d.httpDraining = true; d.httpMu.Unlock() }

func (services *daemonServices) run(ctx context.Context) error {
	if ctx.Err() != nil {
		services.daemon.stopHTTPAdmission()
		if services.grpc != nil {
			services.grpc.Stop()
		}
		if services.httpListener != nil {
			if err := services.httpListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				return err
			}
		}
		return nil
	}
	workerContext, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	// Signal cancellation stops intake, but admitted HTTP handlers receive a
	// grace period. Their connection contexts are canceled on drain expiry.
	requestContext, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	workers := append([]daemonWorker(nil), services.workers...)
	if services.http != nil {
		services.http.BaseContext = func(net.Listener) context.Context { return requestContext }
		workers = append(workers, daemonWorker{"HTTP server", func(context.Context) error { return services.http.Serve(services.httpListener) }})
	}
	if services.grpc != nil {
		workers = append(workers, daemonWorker{"gRPC server", func(context.Context) error { return services.grpc.Start() }})
	}
	events := make(chan daemonServiceEvent, len(workers))
	var running sync.WaitGroup
	for _, worker := range workers {
		running.Add(1)
		go func(worker daemonWorker) {
			defer running.Done()
			events <- daemonServiceEvent{worker.name, worker.run(workerContext)}
		}(worker)
	}
	var cause error
	select {
	case <-ctx.Done():
	case event := <-events:
		if ctx.Err() == nil || !normalServiceStop(event.err) {
			cause = event.failure()
		}
	}
	services.daemon.stopHTTPAdmission()
	cancelWorkers()
	grpcStopped := make(chan struct{})
	go func() {
		defer close(grpcStopped)
		if services.grpc != nil {
			services.grpc.Stop()
		}
	}()
	var drainErr error
	if services.http != nil {
		drainContext, cancel := context.WithTimeout(context.Background(), services.shutdownGrace)
		drainErr = services.http.Shutdown(drainContext)
		cancel()
		if drainErr != nil {
			cancelRequests()
			drainErr = errors.Join(drainErr, services.http.Close())
		}
	}
	cancelRequests()
	// Cancellation is cooperative. Do not close dependencies while a callback
	// still uses them, even if that callback ignores the shutdown deadline.
	services.daemon.httpHandlers.Wait()
	<-grpcStopped
	running.Wait()
	close(events)
	for event := range events {
		if !normalServiceStop(event.err) {
			drainErr = errors.Join(drainErr, event.failure())
		}
	}
	return errors.Join(cause, drainErr)
}
