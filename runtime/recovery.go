package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/effectus/effectus-go/schema"
)

type RecoveryWorker struct {
	Engine        *Engine
	Store         schema.ExecutionLedger
	Owner         string
	BatchSize     int
	LeaseDuration time.Duration
	PollInterval  time.Duration
	Observer      Observer
}

// Run polls until cancellation. Each poll is bounded by BatchSize.
func (worker *RecoveryWorker) Run(ctx context.Context) error {
	interval := worker.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	for {
		if _, err := worker.RunOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// RunOnce leases a bounded set of nonterminal executions and resumes each only
// through Engine.Execute. Lease completion is a CAS performed by the engine.
func (worker *RecoveryWorker) RunOnce(ctx context.Context) (int, error) {
	if worker == nil || worker.Engine == nil || worker.Store == nil {
		return 0, fmt.Errorf("recovery engine and execution ledger are required")
	}
	if strings.TrimSpace(worker.Owner) == "" {
		return 0, fmt.Errorf("recovery worker owner is required")
	}
	batchSize := worker.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
	leaseDuration := worker.LeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}
	leases, err := worker.Store.LeaseExecutions(ctx, worker.Owner, batchSize, leaseDuration)
	if err != nil {
		worker.observe(RecoveryObservation{Err: err})
		return 0, fmt.Errorf("lease recovery executions: %w", err)
	}
	worker.observe(RecoveryObservation{Backlog: len(leases)})
	if len(leases) > batchSize {
		return 0, fmt.Errorf("recovery lease store returned %d executions, limit is %d", len(leases), batchSize)
	}
	processed := 0
	for index := range leases {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		lease := leases[index]
		result, executeErr := worker.Engine.Execute(ctx, ExecuteRequest{ResumeExecutionID: lease.ExecutionID, WaitMode: WaitTerminal, RecoveryLease: &lease})
		processed++
		if executeErr == nil {
			worker.observe(RecoveryObservation{ExecutionID: lease.ExecutionID, State: result.State})
			continue
		}
		observation := RecoveryObservation{ExecutionID: lease.ExecutionID, State: result.State, Err: executeErr}
		worker.observe(observation)
		if errors.Is(executeErr, ErrDurableDisposition) {
			return processed, executeErr
		}
		// Terminal business failures and transient workflow/store errors are
		// per-execution outcomes. Engine.Execute has durably terminalized or
		// released the lease, so unrelated recovery work must continue.
	}
	return processed, nil
}

func (worker *RecoveryWorker) observe(observation RecoveryObservation) {
	if worker != nil && worker.Observer != nil {
		worker.Observer.ObserveRecovery(observation)
	}
}
