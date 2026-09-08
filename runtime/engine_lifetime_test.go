package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/ledger"
	"github.com/stretchr/testify/require"
)

type countedGenerationCloser struct {
	count *atomic.Int64
	err   error
}

func (c countedGenerationCloser) Close() error { c.count.Add(1); return c.err }

func lifetimeGeneration(t *testing.T, version string, executor invocation.Executor, closers ...io.Closer) *Generation {
	t.Helper()
	env := ir.Environment{Verbs: map[string]ir.VerbContract{"Review": {ResultType: "bool"}}}
	checked := compileLanguage(t, env, `rule "review" priority 1 { when { true } then { Review() } }`, "eff")
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorEmbedded})
	require.NoError(t, err)
	generation, err := NewGeneration(GenerationConfig{Checked: checked, Environment: env, Ruleset: "language", Version: version, SourceDigest: strings.Repeat("a", 64), Executors: map[string]invocation.Executor{"Review": executor}, ExecutorDescriptors: map[string]invocation.Descriptor{"Review": descriptor}, Closers: closers})
	require.NoError(t, err)
	return generation
}

func cloneLifetimeGeneration(template *Generation, executor invocation.Executor, closer io.Closer) (*Generation, error) {
	return NewGeneration(GenerationConfig{Checked: template.Checked(), Environment: template.Environment(), Ruleset: template.Ruleset(), Version: template.Version(), SourceDigest: template.SourceDigest(), Executors: map[string]invocation.Executor{"Review": executor}, ExecutorDescriptors: template.ExecutorDescriptors(), Closers: []io.Closer{closer}})
}

func seedHistoricalExecutions(t *testing.T, store schema.ExecutionLedger, outbox schema.OutboxStore, count int) *Generation {
	t.Helper()
	generation := lifetimeGeneration(t, "1", recoveryTestExecutor{})
	engine := languageEngine(t, generation, outbox, store)
	for i := range count {
		_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: fmt.Sprintf("history-%d", i), TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}, WaitMode: WaitAccepted})
		require.NoError(t, err)
	}
	require.NoError(t, engine.Close())
	return generation
}

func TestEngineRejectsAndClosesWrongResolvedGeneration(t *testing.T) {
	store, outbox := schema.NewInMemoryExecutionLedger(), schema.NewInMemoryOutboxStore()
	seedHistoricalExecutions(t, store, outbox, 1)
	var closes atomic.Int64
	wrong := lifetimeGeneration(t, "wrong", recoveryTestExecutor{}, countedGenerationCloser{count: &closes})
	engine, err := NewEngine(lifetimeGeneration(t, "2", recoveryTestExecutor{}))
	require.NoError(t, err)
	require.NoError(t, engine.ConfigureWorkflow(outbox, nil, schema.DispatcherOptions{Owner: "wrong-resolver"}))
	require.NoError(t, engine.ConfigureLedger(store, ArtifactResolverFunc(func(context.Context, ledger.ExecutionArtifact) (*Generation, error) { return wrong, nil })))
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	result, err := engine.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "history-0", WaitMode: WaitTerminal})
	var terminal *TerminalExecutionError
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, schema.ExecutionBlockedDependency, terminal.State)
	require.Equal(t, string(schema.ExecutionBlockedDependency), result.State)
	require.Equal(t, int64(1), closes.Load())
	require.True(t, wrong.Closed())
	engine.mu.Lock()
	require.Empty(t, engine.historical)
	engine.mu.Unlock()
}

func TestEngineMissingHistoricalDependencyHasTypedFirstAndReplayFailure(t *testing.T) {
	store, outbox := schema.NewInMemoryExecutionLedger(), schema.NewInMemoryOutboxStore()
	seedHistoricalExecutions(t, store, outbox, 1)
	engine := languageEngine(t, lifetimeGeneration(t, "2", recoveryTestExecutor{}), outbox, store)
	for range 2 {
		result, err := engine.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "history-0", WaitMode: WaitTerminal})
		var terminal *TerminalExecutionError
		require.ErrorAs(t, err, &terminal)
		require.ErrorIs(t, err, ErrBlockedDependency)
		require.Equal(t, schema.ExecutionBlockedDependency, terminal.State)
		require.Equal(t, string(schema.ExecutionBlockedDependency), result.State)
	}
	result, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "history-0", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.True(t, result.DurablyAccepted)
	require.False(t, result.Completed)
}

func TestEngineHistoricalResolutionSingleFlightAndCloseAfterUse(t *testing.T) {
	store, outbox := schema.NewInMemoryExecutionLedger(), schema.NewInMemoryOutboxStore()
	template := seedHistoricalExecutions(t, store, outbox, 2)
	var historicalCloses, activeCloses, resolves atomic.Int64
	started, finish := make(chan struct{}, 2), make(chan struct{})
	var finishOnce sync.Once
	t.Cleanup(func() { finishOnce.Do(func() { close(finish) }) })
	executor := recoveryExecutorFunc(func(ctx context.Context, _ invocation.Request) invocation.Outcome {
		started <- struct{}{}
		select {
		case <-finish:
			return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
		case <-ctx.Done():
			return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: ctx.Err()}
		}
	})
	engine, err := NewEngine(lifetimeGeneration(t, "2", recoveryTestExecutor{}, countedGenerationCloser{count: &activeCloses}))
	require.NoError(t, err)
	require.NoError(t, engine.ConfigureWorkflow(outbox, nil, schema.DispatcherOptions{Owner: "history"}))
	require.NoError(t, engine.ConfigureLedger(store, ArtifactResolverFunc(func(context.Context, ledger.ExecutionArtifact) (*Generation, error) {
		resolves.Add(1)
		return cloneLifetimeGeneration(template, executor, countedGenerationCloser{count: &historicalCloses})
	})))
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for i := range 2 {
		go func() {
			result, err := engine.Execute(ctx, ExecuteRequest{ResumeExecutionID: fmt.Sprintf("history-%d", i), WaitMode: WaitTerminal})
			if err == nil && (!result.Completed || result.GenerationDigest != template.Digest()) {
				err = fmt.Errorf("historical identity or completion mismatch")
			}
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("historical invocations did not start")
		}
	}
	require.Equal(t, int64(1), resolves.Load())
	require.Zero(t, historicalCloses.Load())
	finishOnce.Do(func() { close(finish) })
	for range 2 {
		require.NoError(t, <-results)
	}
	require.Equal(t, int64(1), historicalCloses.Load())
	engine.mu.Lock()
	require.Empty(t, engine.executions)
	require.Empty(t, engine.historical)
	engine.mu.Unlock()
	require.NoError(t, engine.Close())
	require.NoError(t, engine.Close())
	require.Equal(t, int64(1), activeCloses.Load())
}

func TestEngineCloseWaitsForActiveExecution(t *testing.T) {
	var closes atomic.Int64
	started, finish := make(chan struct{}), make(chan struct{})
	var finishOnce sync.Once
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	executor := recoveryExecutorFunc(func(ctx context.Context, _ invocation.Request) invocation.Outcome {
		close(started)
		select {
		case <-finish:
			return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
		case <-ctx.Done():
			return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: ctx.Err()}
		}
	})
	engine := languageEngine(t, lifetimeGeneration(t, "1", executor, countedGenerationCloser{count: &closes}), schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger())
	t.Cleanup(func() { finishOnce.Do(func() { close(finish) }) })
	executed, closed := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := engine.Execute(ctx, ExecuteRequest{Admission: &Admission{ExecutionID: "active", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}})
		executed <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("execution did not start")
	}
	go func() { closed <- engine.Close() }()
	require.Eventually(t, func() bool { engine.mu.Lock(); defer engine.mu.Unlock(); return engine.closed }, time.Second, time.Millisecond)
	require.Zero(t, closes.Load())
	_, err := engine.Execute(ctx, ExecuteRequest{ResumeExecutionID: "active"})
	require.ErrorContains(t, err, "closed")
	finishOnce.Do(func() { close(finish) })
	require.NoError(t, <-executed)
	require.NoError(t, <-closed)
	require.Equal(t, int64(1), closes.Load())
}

func TestEngineResolutionFailureClosesReturnedGenerationAndPreservesCloseError(t *testing.T) {
	store, outbox := schema.NewInMemoryExecutionLedger(), schema.NewInMemoryOutboxStore()
	template := seedHistoricalExecutions(t, store, outbox, 1)
	var closes atomic.Int64
	closeErr := errors.New("owned closer failed")
	engine, err := NewEngine(lifetimeGeneration(t, "2", recoveryTestExecutor{}))
	require.NoError(t, err)
	require.NoError(t, engine.ConfigureWorkflow(outbox, nil, schema.DispatcherOptions{Owner: "failed-resolver"}))
	require.NoError(t, engine.ConfigureLedger(store, ArtifactResolverFunc(func(context.Context, ledger.ExecutionArtifact) (*Generation, error) {
		generation, err := cloneLifetimeGeneration(template, recoveryTestExecutor{}, countedGenerationCloser{count: &closes, err: closeErr})
		if err != nil {
			return nil, err
		}
		return generation, errors.New("resolver failed after acquisition")
	})))
	_, err = engine.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "history-0"})
	require.ErrorIs(t, err, ErrBlockedDependency)
	require.Equal(t, int64(1), closes.Load())
	require.ErrorIs(t, engine.Close(), closeErr)
	require.ErrorIs(t, engine.Close(), closeErr)
	require.Equal(t, int64(1), closes.Load())
}
