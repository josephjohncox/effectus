package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/ledger"
	"github.com/stretchr/testify/require"
)

func TestHistoricalResolutionCancellationDoesNotBlockExecutions(t *testing.T) {
	for _, cancelLeader := range []bool{false, true} {
		name := "follower"
		if cancelLeader {
			name = "leader"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			var users sync.WaitGroup
			defer func() {
				cancel()
				users.Wait()
			}()
			durable, outbox := schema.NewInMemoryExecutionLedger(), schema.NewInMemoryOutboxStore()
			template := seedHistoricalExecutions(t, durable, outbox, 2)
			var resolves, calls, closes atomic.Int64
			entered, release := make(chan struct{}), make(chan struct{})
			executor := recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
				calls.Add(1)
				return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
			})
			engine := languageEngine(t, lifetimeGeneration(t, "2", recoveryTestExecutor{}), outbox, durable)
			require.NoError(t, engine.ConfigureLedger(durable, ArtifactResolverFunc(func(ctx context.Context, _ ledger.ExecutionArtifact) (*Generation, error) {
				if resolves.Add(1) == 1 {
					close(entered)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-release:
					}
				}
				return cloneLifetimeGeneration(template, executor, countedGenerationCloser{count: &closes})
			})))
			type completion struct {
				result ExecuteResult
				err    error
			}
			run := func(ctx context.Context, id string) <-chan completion {
				done := make(chan completion, 1)
				users.Add(1)
				go func() {
					defer users.Done()
					result, err := engine.Execute(ctx, ExecuteRequest{ResumeExecutionID: id, WaitMode: WaitTerminal})
					done <- completion{result: result, err: err}
				}()
				return done
			}
			leaderCtx, cancelLeaderCtx := context.WithCancel(ctx)
			defer cancelLeaderCtx()
			followerCtx, cancelFollowerCtx := context.WithCancel(ctx)
			defer cancelFollowerCtx()
			leader := run(leaderCtx, "history-0")
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("historical resolver did not start")
			}
			follower := run(followerCtx, "history-1")
			require.Eventually(t, func() bool {
				engine.mu.Lock()
				defer engine.mu.Unlock()
				entry := engine.historical[template.Digest()]
				return entry != nil && entry.users == 2
			}, time.Second, time.Millisecond, "both executions must share the gated resolution")
			before := make([]schema.ExecutionRecord, 2)
			for i := range before {
				var err error
				before[i], err = durable.GetExecution(ctx, fmt.Sprintf("history-%d", i))
				require.NoError(t, err)
			}
			if cancelLeader {
				cancelLeaderCtx()
				for _, done := range []<-chan completion{leader, follower} {
					got := <-done
					require.ErrorIs(t, got.err, context.Canceled)
					require.NotErrorIs(t, got.err, ErrBlockedDependency)
					require.Equal(t, string(schema.ExecutionAccepted), got.result.State)
				}
			} else {
				cancelFollowerCtx()
				got := <-follower
				require.ErrorIs(t, got.err, context.Canceled)
				require.NotErrorIs(t, got.err, ErrBlockedDependency)
				require.Equal(t, string(schema.ExecutionAccepted), got.result.State)
			}
			require.Zero(t, calls.Load())
			for i := range before {
				after, err := durable.GetExecution(ctx, before[i].ExecutionID)
				require.NoError(t, err)
				require.Equal(t, before[i], after, "resolution cancellation must not write terminal state or a new revision")
			}
			close(release)
			if !cancelLeader {
				got := <-leader
				require.NoError(t, got.err)
				require.True(t, got.result.Completed)
			}
			for i := range 2 {
				result, err := engine.Execute(ctx, ExecuteRequest{ResumeExecutionID: fmt.Sprintf("history-%d", i), WaitMode: WaitTerminal})
				require.NoError(t, err)
				require.True(t, result.Completed)
			}
			require.Equal(t, int64(2), calls.Load())
			wantCloses := resolves.Load()
			if cancelLeader {
				wantCloses-- // The canceled resolver returned no generation.
			}
			require.Equal(t, wantCloses, closes.Load())
			engine.mu.Lock()
			empty := len(engine.historical) == 0 && len(engine.executions) == 0
			engine.mu.Unlock()
			require.True(t, empty, "all entered calls and historical generations must be released")
		})
	}
}

func TestHistoricalRecoveryCancellationReleasesLeaseWithoutBlocking(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var users sync.WaitGroup
	defer func() {
		cancel()
		users.Wait()
	}()
	durable, outbox := schema.NewInMemoryExecutionLedger(), schema.NewInMemoryOutboxStore()
	template := seedHistoricalExecutions(t, durable, outbox, 1)
	var resolves, calls, closes atomic.Int64
	entered := make(chan struct{})
	executor := recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		calls.Add(1)
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	})
	engine := languageEngine(t, lifetimeGeneration(t, "2", recoveryTestExecutor{}), outbox, durable)
	require.NoError(t, engine.ConfigureLedger(durable, ArtifactResolverFunc(func(ctx context.Context, _ ledger.ExecutionArtifact) (*Generation, error) {
		if resolves.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return cloneLifetimeGeneration(template, executor, countedGenerationCloser{count: &closes})
	})))
	worker := &RecoveryWorker{Engine: engine, Store: durable, Owner: "historical-cancel", BatchSize: 2, LeaseDuration: time.Minute}
	done := make(chan error, 1)
	users.Add(1)
	go func() {
		defer users.Done()
		_, err := worker.RunOnce(ctx)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery resolver did not start")
	}
	claimed, err := durable.GetExecution(t.Context(), "history-0")
	require.NoError(t, err)
	require.NotEmpty(t, claimed.RecoveryToken)
	cancel()
	err = <-done
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrDurableDisposition)
	record, err := durable.GetExecution(t.Context(), "history-0")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionAccepted, record.State)
	require.Empty(t, record.RecoveryToken)
	require.Empty(t, record.RecoveryOwner)
	require.True(t, record.RecoveryDeadline.IsZero())
	require.Zero(t, calls.Load())
	processed, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int64(1), calls.Load())
	require.Equal(t, int64(1), closes.Load())
	record, err = durable.GetExecution(t.Context(), "history-0")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionCompleted, record.State)
}

type canceledHistoricalArtifactLedger struct {
	ledger.ExecutionLedger
	cause error
}

func (store canceledHistoricalArtifactLedger) GetArtifact(context.Context, string) (ledger.ExecutionArtifact, error) {
	return ledger.ExecutionArtifact{}, store.cause
}

func TestHistoricalArtifactContextErrorsRemainResumable(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, mode := range []struct {
			name      string
			admission bool
			wait      WaitMode
		}{{"resume", false, WaitTerminal}, {"admission-terminal", true, WaitTerminal}, {"admission-accepted", true, WaitAccepted}} {
			t.Run(cause.Error()+"/"+mode.name, func(t *testing.T) {
				durable, outbox := schema.NewInMemoryExecutionLedger(), schema.NewInMemoryOutboxStore()
				seedHistoricalExecutions(t, durable, outbox, 1)
				var calls atomic.Int64
				executor := recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
					calls.Add(1)
					return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
				})
				store := canceledHistoricalArtifactLedger{ExecutionLedger: durable, cause: fmt.Errorf("artifact lookup: %w", cause)}
				engine := languageEngine(t, lifetimeGeneration(t, "2", executor), outbox, store)
				before, err := durable.GetExecution(t.Context(), "history-0")
				require.NoError(t, err)
				request := ExecuteRequest{ResumeExecutionID: before.ExecutionID, WaitMode: mode.wait}
				if mode.admission {
					request.ResumeExecutionID = ""
					request.Admission = &Admission{ExecutionID: before.ExecutionID, AdmissionID: before.AdmissionIdentity, TenantNamespace: before.TenantNamespace, Ruleset: before.Ruleset, Version: before.Version, Facts: map[string]any{}}
				}
				_, err = engine.Execute(t.Context(), request)
				require.ErrorIs(t, err, cause)
				require.NotErrorIs(t, err, ErrBlockedDependency)
				require.NotErrorIs(t, err, ErrTerminalExecution)
				after, err := durable.GetExecution(t.Context(), before.ExecutionID)
				require.NoError(t, err)
				require.Equal(t, before, after, "lookup cancellation must preserve the accepted state and revision")
				require.Zero(t, calls.Load())
				require.NoError(t, engine.Close())

				// A healthy engine can finish the original accepted identity.
				// The failed caller did not need to create a replacement admission.
				generation := lifetimeGeneration(t, "1", executor)
				require.Equal(t, before.GenerationDigest, generation.Digest())
				restarted := languageEngine(t, generation, outbox, durable)
				done, err := restarted.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: before.ExecutionID, WaitMode: WaitTerminal})
				require.NoError(t, err)
				require.True(t, done.Completed)
				require.Equal(t, before.ExecutionID, done.ExecutionID)
				require.Equal(t, int64(1), calls.Load())
			})
		}
	}
}
