package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/ledger"
	"github.com/stretchr/testify/require"
)

type recoveryExecutorFunc func(context.Context, invocation.Request) invocation.Outcome

func (f recoveryExecutorFunc) Invoke(ctx context.Context, r invocation.Request) invocation.Outcome {
	return f(ctx, r)
}

type observedRecoveryLedger struct {
	*schema.InMemoryExecutionLedger
	renewed       chan struct{}
	rejectRenewal bool
	reportedSkew  time.Duration
	renewals      atomic.Int64
	maxClaim      atomic.Int64
}

func (s *observedRecoveryLedger) LeaseExecutions(ctx context.Context, owner string, limit int, duration time.Duration) ([]ledger.ExecutionLease, error) {
	for old := s.maxClaim.Load(); int64(limit) > old; old = s.maxClaim.Load() {
		if s.maxClaim.CompareAndSwap(old, int64(limit)) {
			break
		}
	}
	leases, err := s.InMemoryExecutionLedger.LeaseExecutions(ctx, owner, limit, duration)
	for i := range leases {
		leases[i].Deadline = leases[i].Deadline.Add(s.reportedSkew)
	}
	return leases, err
}

func (s *observedRecoveryLedger) RenewExecutionLease(ctx context.Context, lease ledger.ExecutionLease, duration time.Duration) (ledger.ExecutionLease, error) {
	// Engine authority probes are not periodic lifetime-extension attempts.
	if duration == time.Microsecond {
		return s.InMemoryExecutionLedger.RenewExecutionLease(ctx, lease, duration)
	}
	if s.renewals.Add(1) > 1 && s.rejectRenewal {
		return ledger.ExecutionLease{}, schema.ErrStaleExecutionLease
	}
	renewed, err := s.InMemoryExecutionLedger.RenewExecutionLease(ctx, lease, duration)
	if err == nil && s.renewed != nil {
		select {
		case s.renewed <- struct{}{}:
		default:
		}
	}
	renewed.Deadline = renewed.Deadline.Add(s.reportedSkew)
	return renewed, err
}

func recoveryFixture(t *testing.T, durable ledger.ExecutionLedger, count int, executor invocation.Executor) *Engine {
	t.Helper()
	env := ir.Environment{Verbs: map[string]ir.VerbContract{"Review": {ResultType: "bool"}}}
	checked := compileLanguage(t, env, `rule "review" priority 1 { when { true } then { Review() } }`, "eff")
	generation := languageGeneration(t, env, checked, map[string]invocation.Executor{"Review": executor})
	engine := languageEngine(t, generation, schema.NewInMemoryOutboxStore(), durable)
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("item-%d", index)
		_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: id, AdmissionID: id, TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}, WaitMode: WaitAccepted})
		require.NoError(t, err)
	}
	return engine
}

func TestRecoveryRenewsLongExecutionWithoutLeasingBatchAhead(t *testing.T) {
	for _, skew := range []time.Duration{0, -24 * time.Hour, 24 * time.Hour} {
		t.Run(skew.String(), func(t *testing.T) { checkLongRecovery(t, skew) })
	}
}

func checkLongRecovery(t *testing.T, skew time.Duration) {
	t.Helper()
	durable := &observedRecoveryLedger{InMemoryExecutionLedger: schema.NewInMemoryExecutionLedger(), renewed: make(chan struct{}, 16), reportedSkew: skew}
	calls := 0
	engine := recoveryFixture(t, durable, 2, recoveryExecutorFunc(func(ctx context.Context, request invocation.Request) invocation.Outcome {
		calls++
		if request.Metadata.ExecutionID == "item-0" {
			next, err := durable.GetExecution(ctx, "item-1")
			require.NoError(t, err)
			require.Empty(t, next.RecoveryToken, "later batch work must not hold an idle lease")
		}
		for range 5 {
			select {
			case <-durable.renewed:
			case <-ctx.Done():
				return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: ctx.Err()}
			}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	worker := &RecoveryWorker{Engine: engine, Store: durable, Owner: "worker", BatchSize: 2, LeaseDuration: 90 * time.Millisecond}
	processed, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, processed)
	require.Equal(t, 2, calls)
	require.Equal(t, int64(1), durable.maxClaim.Load())
	for index := range 2 {
		record, err := durable.GetExecution(ctx, fmt.Sprintf("item-%d", index))
		require.NoError(t, err)
		require.Equal(t, schema.ExecutionCompleted, record.State)
		require.Empty(t, record.RecoveryToken)
	}
}

func TestRecoveryRenewalFailureCancelsWorkAndStopsFurtherInvocation(t *testing.T) {
	durable := &observedRecoveryLedger{InMemoryExecutionLedger: schema.NewInMemoryExecutionLedger(), rejectRenewal: true}
	calls := 0
	engine := recoveryFixture(t, durable, 1, recoveryExecutorFunc(func(ctx context.Context, _ invocation.Request) invocation.Outcome {
		calls++
		<-ctx.Done()
		return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: ctx.Err()}
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	worker := &RecoveryWorker{Engine: engine, Store: durable, Owner: "worker", BatchSize: 8, LeaseDuration: 300 * time.Millisecond}
	processed, err := worker.RunOnce(ctx)
	// Per-execution nonterminal outcomes do not crash unrelated recovery work.
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, 1, calls)
	record, err := durable.GetExecution(ctx, "item-0")
	require.NoError(t, err)
	require.Empty(t, record.RecoveryToken)
	require.NotEqual(t, schema.ExecutionCompleted, record.State)
}

func TestRecoveryCancellationReleasesLease(t *testing.T) {
	durable := schema.NewInMemoryExecutionLedger()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	engine := recoveryFixture(t, durable, 1, recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		cancel()
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	}))
	worker := &RecoveryWorker{Engine: engine, Store: durable, Owner: "worker", BatchSize: 1}
	_, err := worker.RunOnce(ctx)
	require.False(t, errors.Is(err, ErrDurableDisposition), "%v", err)
	record, err := durable.GetExecution(t.Context(), "item-0")
	require.NoError(t, err)
	require.Empty(t, record.RecoveryToken)
}

func TestRecoveryRejectsReplacedLeaseBeforeInvocation(t *testing.T) {
	durable := schema.NewInMemoryExecutionLedger()
	calls := 0
	engine := recoveryFixture(t, durable, 1, recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		calls++
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	}))
	leases, err := durable.LeaseExecutions(t.Context(), "stale", 1, time.Minute)
	require.NoError(t, err)
	require.NoError(t, durable.FinishExecutionLease(t.Context(), leases[0], "", ""))
	_, err = durable.LeaseExecutions(t.Context(), "current", 1, time.Minute)
	require.NoError(t, err)
	worker := &RecoveryWorker{Engine: engine, Store: durable, Owner: "stale"}
	_, err = worker.executeLease(t.Context(), leases[0], time.Minute, time.Now().Add(time.Minute))
	require.ErrorIs(t, err, errRecoveryLeaseLost)
	require.ErrorIs(t, err, schema.ErrStaleExecutionLease)
	require.Zero(t, calls)
}

type blockingRecoveryRenewer struct {
	*schema.InMemoryExecutionLedger
	calls   atomic.Int64
	started chan struct{}
}

func (s *blockingRecoveryRenewer) RenewExecutionLease(ctx context.Context, lease ledger.ExecutionLease, duration time.Duration) (ledger.ExecutionLease, error) {
	if duration == time.Microsecond {
		return s.InMemoryExecutionLedger.RenewExecutionLease(ctx, lease, duration)
	}
	if s.calls.Add(1) == 1 {
		return s.InMemoryExecutionLedger.RenewExecutionLease(ctx, lease, duration)
	}
	close(s.started)
	<-ctx.Done()
	return ledger.ExecutionLease{}, ctx.Err()
}

func TestRecoveryCompletionJoinsInflightRenewal(t *testing.T) {
	durable := &blockingRecoveryRenewer{InMemoryExecutionLedger: schema.NewInMemoryExecutionLedger(), started: make(chan struct{})}
	engine := recoveryFixture(t, durable, 1, recoveryExecutorFunc(func(ctx context.Context, _ invocation.Request) invocation.Outcome {
		select {
		case <-durable.started:
			return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
		case <-ctx.Done():
			return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: ctx.Err()}
		}
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	worker := &RecoveryWorker{Engine: engine, Store: durable, Owner: "worker", BatchSize: 1, LeaseDuration: 300 * time.Millisecond}
	processed, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	record, err := durable.GetExecution(ctx, "item-0")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionCompleted, record.State)
	require.Equal(t, int64(2), durable.calls.Load())
}

func TestRecoveryDefaultAndValidSettings(t *testing.T) {
	durable := schema.NewInMemoryExecutionLedger()
	engine := recoveryFixture(t, durable, 0, recoveryTestExecutor{})
	for _, configured := range []bool{false, true} {
		worker := &RecoveryWorker{Engine: engine, Store: durable, Owner: "worker"}
		if configured {
			worker.BatchSize, worker.LeaseDuration, worker.PollInterval = 2, time.Second, time.Millisecond
		}
		batch, duration, interval, err := worker.settings(t.Context())
		require.NoError(t, err)
		if configured {
			require.Equal(t, 2, batch)
			require.Equal(t, time.Second, duration)
			require.Equal(t, time.Millisecond, interval)
		} else {
			require.Equal(t, 32, batch)
			require.Equal(t, 30*time.Second, duration)
			require.Equal(t, time.Second, interval)
		}
	}
}

func TestRecoveryRejectsNegativeSettings(t *testing.T) {
	durable := schema.NewInMemoryExecutionLedger()
	engine := recoveryFixture(t, durable, 0, recoveryTestExecutor{})
	for _, worker := range []*RecoveryWorker{
		{Engine: engine, Store: durable, Owner: "worker", BatchSize: -1},
		{Engine: engine, Store: durable, Owner: "worker", LeaseDuration: -1},
		{Engine: engine, Store: durable, Owner: "worker", PollInterval: -1},
		nil,
	} {
		_, err := worker.RunOnce(t.Context())
		require.Error(t, err)
		require.Error(t, worker.Run(t.Context()))
	}
}
