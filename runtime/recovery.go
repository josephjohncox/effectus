package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/josephjohncox/effectus/schema/ledger"
)

// RecoveryWorker resumes durable executions serially. BatchSize bounds work per
// poll, not the number of leases held at once. Configure fields before Run.
// Built-in stores renew live leases. Custom stores without renewal support must
// finish each execution within the original lease's safe window.
// Zero BatchSize, LeaseDuration, and PollInterval select 32, 30s, and 1s.
// Negative settings are invalid; leases must be at least one microsecond.
type RecoveryWorker struct {
	Engine        *Engine
	Store         ledger.ExecutionLedger
	Owner         string
	BatchSize     int
	LeaseDuration time.Duration
	PollInterval  time.Duration
	Observer      Observer
}

func (worker *RecoveryWorker) settings(ctx context.Context) (int, time.Duration, time.Duration, error) {
	if worker == nil || ctx == nil || worker.Engine == nil || worker.Store == nil {
		return 0, 0, 0, fmt.Errorf("recovery engine, execution ledger, and context are required")
	}
	if strings.TrimSpace(worker.Owner) == "" {
		return 0, 0, 0, fmt.Errorf("recovery worker owner is required")
	}
	if worker.BatchSize < 0 || worker.LeaseDuration < 0 || worker.PollInterval < 0 {
		return 0, 0, 0, fmt.Errorf("recovery sizes and durations must not be negative")
	}
	batch, duration, interval := worker.BatchSize, worker.LeaseDuration, worker.PollInterval
	if batch == 0 {
		batch = 32
	}
	if duration == 0 {
		duration = 30 * time.Second
	}
	if duration < time.Microsecond {
		return 0, 0, 0, fmt.Errorf("recovery lease must be at least one microsecond")
	}
	if interval == 0 {
		interval = time.Second
	}
	return batch, duration, interval, nil
}

// Run polls until cancellation. Each poll is bounded by BatchSize.
func (worker *RecoveryWorker) Run(ctx context.Context) error {
	_, _, interval, err := worker.settings(ctx)
	if err != nil {
		return err
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

// RunOnce acquires a lease only when it can start that execution. It resumes
// work through Engine.Execute and leaves terminal persistence to the engine.
func (worker *RecoveryWorker) RunOnce(ctx context.Context) (int, error) {
	batchSize, leaseDuration, _, err := worker.settings(ctx)
	if err != nil {
		return 0, err
	}
	if reader, ok := worker.Store.(ledger.RecoveryStatsReader); ok {
		stats, statsErr := reader.RecoveryStats(ctx)
		if statsErr != nil {
			worker.observe(RecoveryObservation{Err: statsErr})
			return 0, fmt.Errorf("measure recovery backlog: %w", statsErr)
		}
		now := time.Now()
		observation := RecoveryObservation{BacklogMeasured: true, Backlog: stats.Nonterminal, Blocked: stats.Blocked}
		if !stats.OldestNonterminal.IsZero() {
			observation.OldestExecutionAge = now.Sub(stats.OldestNonterminal)
		}
		if !stats.OldestOutbox.IsZero() {
			observation.OldestOutboxAge = now.Sub(stats.OldestOutbox)
		}
		worker.observe(observation)
	}
	processed := 0
	for processed < batchSize {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		localDeadline := time.Now().Add(leaseDuration)
		leases, err := worker.Store.LeaseExecutions(ctx, worker.Owner, 1, leaseDuration)
		if err != nil {
			worker.observe(RecoveryObservation{Err: err})
			return processed, fmt.Errorf("lease recovery execution: %w", err)
		}
		if len(leases) == 0 {
			break
		}
		if len(leases) != 1 {
			return processed, fmt.Errorf("recovery lease store returned %d executions, limit is 1", len(leases))
		}
		lease := leases[0]
		result, executeErr := worker.executeLease(ctx, lease, leaseDuration, localDeadline)
		processed++
		worker.observe(RecoveryObservation{ExecutionID: lease.ExecutionID, State: result.State, Err: executeErr})
		if errors.Is(executeErr, errRecoveryLeaseLost) {
			// Back off until the next poll instead of reacquiring and invoking
			// the same execution repeatedly during a renewal outage.
			return processed, nil
		}
		if errors.Is(executeErr, ErrDurableDisposition) {
			return processed, executeErr
		}
		// Known business failures do not stop unrelated recovery work.
	}
	return processed, nil
}

var errRecoveryLeaseLost = errors.New("recovery lease authority lost")

func (worker *RecoveryWorker) executeLease(ctx context.Context, lease ledger.ExecutionLease, duration time.Duration, localDeadline time.Time) (ExecuteResult, error) {
	renewer, ok := worker.Store.(ledger.ExecutionLeaseRenewer)
	if !ok {
		callCtx, cancel := context.WithDeadline(ctx, localDeadline.Add(-duration/10))
		defer cancel()
		return worker.Engine.Execute(callCtx, ExecuteRequest{ResumeExecutionID: lease.ExecutionID, WaitMode: WaitTerminal, RecoveryLease: &lease})
	}
	// Confirm authority synchronously, before loading or invoking the workflow.
	// Derive deadlines from local monotonic time before each RPC, never from
	// database wall-clock timestamps. The store remains the CAS authority.
	var err error
	localDeadline, err = renewRecoveryLease(ctx, renewer, lease, duration, localDeadline)
	if err != nil {
		return ExecuteResult{}, err
	}
	callCtx, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		deadline := localDeadline
		for {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				cancel(errRecoveryLeaseLost)
				return
			}
			timer := time.NewTimer(remaining / 3)
			select {
			case <-callCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			next, err := renewRecoveryLease(callCtx, renewer, lease, duration, deadline)
			if err != nil {
				cancel(err)
				return
			}
			deadline = next
		}
	}()
	result, err := worker.Engine.Execute(callCtx, ExecuteRequest{ResumeExecutionID: lease.ExecutionID, WaitMode: WaitTerminal, RecoveryLease: &lease})
	cause := context.Cause(callCtx)
	cancel(nil)
	<-done
	if err != nil && cause != nil {
		err = errors.Join(err, cause)
	}
	return result, err
}

func renewRecoveryLease(ctx context.Context, renewer ledger.ExecutionLeaseRenewer, lease ledger.ExecutionLease, duration time.Duration, deadline time.Time) (time.Time, error) {
	renewCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	nextDeadline := time.Now().Add(duration)
	renewed, err := renewer.RenewExecutionLease(renewCtx, lease, duration)
	if err != nil {
		return time.Time{}, errors.Join(errRecoveryLeaseLost, err)
	}
	if err := renewCtx.Err(); err != nil {
		return time.Time{}, errors.Join(errRecoveryLeaseLost, err)
	}
	if renewed.ExecutionID != lease.ExecutionID || renewed.Owner != lease.Owner || renewed.Token != lease.Token || renewed.Revision != lease.Revision || !nextDeadline.After(time.Now()) {
		return time.Time{}, fmt.Errorf("%w: renewal changed lease identity or exceeded its duration", errRecoveryLeaseLost)
	}
	return nextDeadline, nil
}

func (worker *RecoveryWorker) observe(observation RecoveryObservation) {
	if worker != nil && worker.Observer != nil {
		worker.Observer.ObserveRecovery(observation)
	}
}
