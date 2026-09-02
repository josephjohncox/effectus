package postgres

import (
	"context"
	"testing"
	"time"
)

func TestCDCStopSerializesLifecycleGeneration(t *testing.T) {
	workerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	source := &CDCSource{config: &CDCConfig{SourceID: "lifecycle", ConnectionString: "postgres://invalid"}, ctx: workerCtx, cancel: cancel, done: done, running: true}

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
	if err := source.Stop(context.Background()); err != nil {
		t.Fatalf("idempotent Stop failed: %v", err)
	}
}
