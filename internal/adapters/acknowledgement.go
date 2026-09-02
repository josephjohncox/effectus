package adapters

import (
	"context"
	"fmt"
	"sync"
)

// AcknowledgementBarrier commits a source checkpoint only after every fact in
// one source record has crossed the caller's durable boundary.
type AcknowledgementBarrier struct {
	mu        sync.Mutex
	acked     []bool
	remaining int
	committed bool
	commit    func(context.Context) error
	done      chan struct{}
}

// NewAcknowledgementBarrier creates one idempotent callback per emitted fact.
// The commit function must itself be idempotent because a failed response can
// leave its durable outcome unknown.
func NewAcknowledgementBarrier(count int, commit func(context.Context) error) *AcknowledgementBarrier {
	if count < 1 {
		panic("acknowledgement barrier requires at least one fact")
	}
	if commit == nil {
		panic("acknowledgement barrier requires a commit function")
	}
	return &AcknowledgementBarrier{acked: make([]bool, count), remaining: count, commit: commit, done: make(chan struct{})}
}

// Callback returns the retry-safe durable acknowledgement for one fact.
func (barrier *AcknowledgementBarrier) Callback(index int) func(context.Context) error {
	return func(ctx context.Context) error {
		barrier.mu.Lock()
		defer barrier.mu.Unlock()
		if index < 0 || index >= len(barrier.acked) {
			return fmt.Errorf("acknowledgement index %d is out of range", index)
		}
		if barrier.committed {
			return nil
		}
		if !barrier.acked[index] {
			barrier.acked[index] = true
			barrier.remaining--
		}
		if barrier.remaining != 0 {
			return nil
		}
		if err := barrier.commit(ctx); err != nil {
			return err
		}
		barrier.committed = true
		close(barrier.done)
		return nil
	}
}

// Wait blocks source progress until the complete record is durably committed.
func (barrier *AcknowledgementBarrier) Wait(ctx context.Context) error {
	select {
	case <-barrier.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
