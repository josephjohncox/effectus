package grpc

import (
	"context"
	"testing"
	"time"
)

func TestStopSerializesLifecycleGeneration(t *testing.T) {
	workerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	source := &Source{config: &Config{SourceID: "lifecycle", Address: "invalid", Method: "/x/y"}, ctx: workerCtx, cancel: cancel, done: done, running: true}

	healthDone := make(chan struct{})
	go func() {
		for {
			select {
			case <-healthDone:
				return
			default:
				_ = source.HealthCheck()
			}
		}
	}()
	stopped := make(chan error, 1)
	go func() { stopped <- source.Stop(context.Background()) }()
	for deadline := time.Now().Add(time.Second); ; {
		source.mu.Lock()
		stopping := source.stopping
		source.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Stop did not enter stopping state")
		}
	}
	if err := source.Start(context.Background()); err == nil {
		t.Fatal("Start accepted an overlapping generation")
	}
	close(done)
	if err := <-stopped; err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	close(healthDone)
	if err := source.Stop(context.Background()); err != nil {
		t.Fatalf("idempotent Stop failed: %v", err)
	}
}
