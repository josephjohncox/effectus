package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/ledger"
)

var (
	ErrTerminalExecution = errors.New("execution ended unsuccessfully")
	ErrExecutionBusy     = errors.New("execution has a recovery owner")
)

// TerminalExecutionError preserves a durable failed or blocked disposition.
// Inspect State with errors.As and Cause with errors.Is/Unwrap. Cause may contain
// executor or storage details; transports must not expose it without sanitizing.
type TerminalExecutionError struct {
	State schema.ExecutionState
	Cause error
}

func (err *TerminalExecutionError) Error() string {
	if err.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", ErrTerminalExecution, err.State, err.Cause)
	}
	return fmt.Sprintf("%s: %s", ErrTerminalExecution, err.State)
}
func (err *TerminalExecutionError) Unwrap() error { return err.Cause }
func (err *TerminalExecutionError) Is(target error) bool {
	return target == ErrTerminalExecution || (err.State == schema.ExecutionBlockedDependency && target == ErrBlockedDependency)
}

func terminalExecutionResult(record schema.ExecutionRecord, wait WaitMode, cause error) (ExecuteResult, error) {
	if schema.IsTerminalExecutionState(record.State) {
		if wait == WaitAccepted || record.State == schema.ExecutionCompleted {
			return engineResult(record), nil
		}
		if cause == nil && record.LastError != "" {
			cause = errors.New(record.LastError)
		}
		return engineResult(record), &TerminalExecutionError{State: record.State, Cause: cause}
	}
	return engineResult(record), cause
}

func adoptExecutionRecord(execution *engineExecution, next schema.ExecutionRecord) error {
	old := execution.record
	if next.ExecutionID != old.ExecutionID || next.AdmissionIdentity != old.AdmissionIdentity || next.GenerationDigest != old.GenerationDigest || next.RequestHash != old.RequestHash || next.Ruleset != old.Ruleset || next.Version != old.Version || next.TenantNamespace != old.TenantNamespace {
		return fmt.Errorf("%w: durable execution identity changed", ErrIdentityConflict)
	}
	if next.Revision < old.Revision {
		return fmt.Errorf("%w: durable revision moved backwards", schema.ErrOptimisticConflict)
	}
	execution.record = next
	return nil
}

func (engine *Engine) refreshExecution(ctx context.Context, execution *engineExecution, lease *schema.ExecutionLease) error {
	next, err := engine.ledger.GetExecution(ctx, execution.record.ExecutionID)
	if err != nil {
		return err
	}
	if err := adoptExecutionRecord(execution, next); err != nil {
		return err
	}
	if schema.IsTerminalExecutionState(next.State) {
		return nil
	}
	if lease == nil {
		if next.RecoveryToken != "" {
			return ErrExecutionBusy
		}
		return nil
	}
	if lease.ExecutionID != next.ExecutionID || lease.Owner != next.RecoveryOwner || lease.Token == "" || lease.Token != next.RecoveryToken || lease.Revision != next.Revision {
		return fmt.Errorf("%w: %w", ErrDurableDisposition, schema.ErrStaleExecutionLease)
	}
	if renewer, ok := engine.ledger.(ledger.ExecutionLeaseRenewer); ok {
		// Non-shrinking renewal with the minimum duration checks current store
		// authority without deriving deadlines from a database wall clock.
		if _, err := renewer.RenewExecutionLease(ctx, *lease, time.Microsecond); err != nil {
			return fmt.Errorf("%w: %w", ErrDurableDisposition, err)
		}
	}
	return ctx.Err()
}

func (engine *Engine) commitExecutionState(ctx context.Context, execution *engineExecution, state schema.ExecutionState, message string, lease *schema.ExecutionLease) error {
	if lease != nil {
		next := state
		if !schema.IsTerminalExecutionState(state) {
			next = ""
		}
		err := engine.ledger.FinishExecutionLease(ctx, *lease, next, message)
		if err != nil {
			if errors.Is(err, schema.ErrStaleExecutionLease) {
				current, readErr := engine.ledger.GetExecution(ctx, execution.record.ExecutionID)
				if readErr == nil && schema.IsTerminalExecutionState(current.State) {
					return adoptExecutionRecord(execution, current)
				}
			}
			return fmt.Errorf("%w: %w", ErrDurableDisposition, err)
		}
		updated, err := engine.ledger.GetExecution(ctx, execution.record.ExecutionID)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrDurableDisposition, err)
		}
		return adoptExecutionRecord(execution, updated)
	}
	for range 3 {
		if schema.IsTerminalExecutionState(execution.record.State) {
			return nil
		}
		if execution.record.RecoveryToken != "" {
			return ErrExecutionBusy
		}
		updated, err := engine.ledger.SetExecutionState(ctx, execution.record.ExecutionID, execution.record.Revision, state, message)
		if err == nil {
			return adoptExecutionRecord(execution, updated)
		}
		if !errors.Is(err, schema.ErrOptimisticConflict) {
			return err
		}
		updated, err = engine.ledger.GetExecution(ctx, execution.record.ExecutionID)
		if err != nil {
			return err
		}
		if err := adoptExecutionRecord(execution, updated); err != nil {
			return err
		}
	}
	if schema.IsTerminalExecutionState(execution.record.State) {
		return nil
	}
	if execution.record.RecoveryToken != "" {
		return ErrExecutionBusy
	}
	return fmt.Errorf("%w: execution state remained contended", schema.ErrOptimisticConflict)
}
