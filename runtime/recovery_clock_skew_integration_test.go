//go:build integration && clockskew

package runtime

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/ledger"
	"github.com/stretchr/testify/require"
)

// Limit this test's poll to its own execution. All claims, renewals, and
// deadlines still come from PostgreSQL; the wrapper never rewrites them.
type clockSkewRecoveryLedger struct {
	*schema.PostgresOutboxStore
	executionID string
	claimed     chan ledger.ExecutionLease
	renewals    atomic.Int32
}

func (store *clockSkewRecoveryLedger) LeaseExecutions(ctx context.Context, owner string, _ int, duration time.Duration) ([]ledger.ExecutionLease, error) {
	lease, err := store.LeaseExecution(ctx, store.executionID, owner, duration)
	if err != nil {
		return nil, err
	}
	store.claimed <- lease
	return []ledger.ExecutionLease{lease}, nil
}

func (store *clockSkewRecoveryLedger) RenewExecutionLease(ctx context.Context, lease ledger.ExecutionLease, duration time.Duration) (ledger.ExecutionLease, error) {
	renewed, err := store.PostgresOutboxStore.RenewExecutionLease(ctx, lease, duration)
	if err == nil {
		store.renewals.Add(1)
	}
	return renewed, err
}

func clockSkewDatabaseNow(t *testing.T, ctx context.Context, db *sql.DB, offset time.Duration) time.Time {
	t.Helper()
	before := time.Now()
	var remote time.Time
	require.NoError(t, db.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&remote))
	after := time.Now()
	require.False(t, remote.Before(before.Add(offset-time.Second)), "database must have the selected independent clock offset")
	require.False(t, remote.After(after.Add(offset+time.Second)), "database must have the selected independent clock offset")
	t.Logf("database clock offset from caller: %s (expected %s)", remote.Sub(before), offset)
	return remote
}

func TestPostgresRecoveryWithIndependentDatabaseClock(t *testing.T) {
	dsn := os.Getenv("EFFECTUS_CLOCK_POSTGRES_DSN")
	require.NotEmpty(t, dsn, "the clockskew tag requires a dedicated clock-offset PostgreSQL fixture")
	var offset time.Duration
	switch os.Getenv("EFFECTUS_CLOCK_OFFSET_SECONDS") {
	case "86400":
		offset = 24 * time.Hour
	case "-86400":
		offset = -24 * time.Hour
	default:
		t.Fatal("EFFECTUS_CLOCK_OFFSET_SECONDS must be 86400 or -86400")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.PingContext(ctx))
	clockSkewDatabaseNow(t, ctx, db, offset)
	require.NoError(t, schema.MigrateSagaV2(ctx, db))
	durable, err := schema.NewPostgresOutboxStore(db)
	require.NoError(t, err)

	var calls atomic.Int32
	var signalEntry sync.Once
	entered, release := make(chan struct{}), make(chan struct{})
	executor := recoveryExecutorFunc(func(ctx context.Context, _ invocation.Request) invocation.Outcome {
		calls.Add(1)
		signalEntry.Do(func() { close(entered) })
		select {
		case <-ctx.Done():
			return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: ctx.Err()}
		case <-release:
			return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
		}
	})
	environment := ir.Environment{Verbs: map[string]ir.VerbContract{"Review": {ResultType: "bool"}}}
	checked := compileLanguage(t, environment, `rule "review" priority 1 { when { true } then { Review() } }`, "eff")
	generation := languageGeneration(t, environment, checked, map[string]invocation.Executor{"Review": executor})
	engine := languageEngine(t, generation, durable, durable)
	id := "clock-" + uuid.NewString()
	request := ExecuteRequest{Admission: &Admission{ExecutionID: id, AdmissionID: id, TenantNamespace: "clock-fixture", Ruleset: "language", Version: "1", Facts: map[string]any{}}, WaitMode: WaitAccepted}
	accepted, err := engine.Execute(ctx, request)
	require.NoError(t, err)
	require.True(t, accepted.DurablyAccepted)
	require.False(t, accepted.Completed)
	require.Zero(t, calls.Load())

	const leaseDuration = 400 * time.Millisecond
	store := &clockSkewRecoveryLedger{PostgresOutboxStore: durable, executionID: id, claimed: make(chan ledger.ExecutionLease, 1)}
	worker := &RecoveryWorker{Engine: engine, Store: store, Owner: "clock-worker", BatchSize: 1, LeaseDuration: leaseDuration}
	type runResult struct {
		processed int
		err       error
	}
	done := make(chan runResult, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		processed, err := worker.RunOnce(ctx)
		done <- runResult{processed: processed, err: err}
	}()
	select {
	case <-entered:
	case result := <-done:
		t.Fatalf("recovery stopped before executor entry: %+v", result)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	original := <-store.claimed
	// Cross several real lease windows while the executor remains live.
	timer := time.NewTimer(4 * leaseDuration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case result := <-done:
		t.Fatalf("recovery stopped during long execution: %+v", result)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.GreaterOrEqual(t, store.renewals.Load(), int32(3), "renewal must use elapsed caller time, not the shifted database deadline")
	_, err = durable.LeaseExecution(ctx, id, "competitor", leaseDuration)
	require.ErrorIs(t, err, schema.ErrStaleExecutionLease, "live renewal must prevent a competing claim after the original lease window")
	record, err := durable.GetExecution(ctx, id)
	require.NoError(t, err)
	require.Equal(t, original.Owner, record.RecoveryOwner)
	require.Equal(t, original.Token, record.RecoveryToken)
	remote := clockSkewDatabaseNow(t, ctx, db, offset)
	require.True(t, record.RecoveryDeadline.After(remote))
	close(release)
	result := <-done
	workers.Wait()
	require.NoError(t, result.err)
	require.Equal(t, 1, result.processed)
	require.NoError(t, ctx.Err())
	terminal, err := durable.GetExecution(ctx, id)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionCompleted, terminal.State)
	require.Empty(t, terminal.RecoveryOwner)
	require.Empty(t, terminal.RecoveryToken)
	require.True(t, terminal.RecoveryDeadline.IsZero())
	_, err = durable.RenewExecutionLease(ctx, original, leaseDuration)
	require.ErrorIs(t, err, schema.ErrStaleExecutionLease)
	request.WaitMode = WaitTerminal
	replayed, err := engine.Execute(ctx, request)
	require.NoError(t, err)
	require.True(t, replayed.Completed)
	require.Equal(t, id, replayed.ExecutionID)
	require.Equal(t, int32(1), calls.Load(), "terminal replay must not invoke again")
}
