package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
)

type remediationBenchmarkCase struct {
	name         string
	plans        int
	payloadBytes int
}

var remediationBenchmarkCases = []remediationBenchmarkCase{
	{name: "small", plans: 1, payloadBytes: 256},
	{name: "medium", plans: 16, payloadBytes: 1024},
	{name: "large", plans: 128, payloadBytes: 4096},
}

type remediationBenchmarkFixture struct {
	config       remediationBenchmarkCase
	source       *bundle.SourceBundle
	sourceDigest string
	environment  ir.Environment
	checked      *ir.Checked
	facts        map[string]any
}

func newRemediationBenchmarkFixture(tb testing.TB, config remediationBenchmarkCase) remediationBenchmarkFixture {
	tb.Helper()
	environment := ir.Environment{
		Facts: map[string]string{"order.id": "string", "order.risk": "int", "order.payload": "string"},
		Verbs: map[string]ir.VerbContract{"Review": {
			Arguments: map[string]string{"orderId": "string", "payload": "string"}, ResultType: "bool",
		}},
	}
	var rules strings.Builder
	for index := 1; index <= config.plans; index++ {
		fmt.Fprintf(&rules, "rule \"review-%03d\" priority %d { when { order.risk >= %d } then { Review(orderId: order.id, payload: order.payload) } }\n", index, index, index)
	}
	source, err := bundle.New(bundle.Spec{Name: "remediation-benchmark", Version: "1", Environment: environment, Sources: []bundle.Source{{Path: "review.eff", Content: rules.String()}}})
	if err != nil {
		tb.Fatal(err)
	}
	checked, err := compiler.CompileChecked(context.Background(), source, compiler.CompileOptions{})
	if err != nil {
		tb.Fatal(err)
	}
	if checked.PlanCount() != config.plans || checked.StepCount() != config.plans {
		tb.Fatal("benchmark must contain one real step per plan")
	}
	digest, err := source.Digest()
	if err != nil {
		tb.Fatal(err)
	}
	return remediationBenchmarkFixture{config: config, source: source, sourceDigest: digest, environment: environment, checked: checked, facts: map[string]any{
		"order.id": "order-1", "order.risk": int64(config.plans), "order.payload": strings.Repeat("x", config.payloadBytes),
	}}
}

type remediationBenchmarkExecutor struct{ calls atomic.Int64 }

func (executor *remediationBenchmarkExecutor) Invoke(ctx context.Context, _ invocation.Request) invocation.Outcome {
	if err := ctx.Err(); err != nil {
		return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: err}
	}
	executor.calls.Add(1)
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
}

func newRemediationBenchmarkEngine(tb testing.TB, fixture remediationBenchmarkFixture) (*Engine, *schema.InMemoryExecutionLedger, *remediationBenchmarkExecutor) {
	tb.Helper()
	executor := &remediationBenchmarkExecutor{}
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorEmbedded})
	if err != nil {
		tb.Fatal(err)
	}
	generation, err := NewGeneration(GenerationConfig{Checked: fixture.checked, Environment: fixture.environment,
		Ruleset: fixture.source.Name(), Version: fixture.source.Version(), SourceDigest: fixture.sourceDigest,
		Executors: map[string]invocation.Executor{"Review": executor}, ExecutorDescriptors: map[string]invocation.Descriptor{"Review": descriptor},
	})
	if err != nil {
		tb.Fatal(err)
	}
	engine, err := NewEngine(generation)
	if err != nil {
		_ = generation.Close()
		tb.Fatal(err)
	}
	durable := schema.NewInMemoryExecutionLedger()
	if err := engine.ConfigureWorkflow(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "benchmark"}); err != nil {
		_ = engine.Close()
		tb.Fatal(err)
	}
	if err := engine.ConfigureLedger(durable, nil); err != nil {
		_ = engine.Close()
		tb.Fatal(err)
	}
	return engine, durable, executor
}

func closeRemediationBenchmarkEngine(tb testing.TB, engine *Engine) {
	tb.Helper()
	if err := engine.Close(); err != nil {
		tb.Fatal(err)
	}
}

func remediationBenchmarkRequest(fixture remediationBenchmarkFixture, id string, wait WaitMode) ExecuteRequest {
	return ExecuteRequest{Admission: &Admission{ExecutionID: id, AdmissionID: id, TenantNamespace: "benchmark", Ruleset: fixture.source.Name(), Version: fixture.source.Version(), Facts: fixture.facts}, WaitMode: wait}
}

func BenchmarkRemediationCheckedCompilation(b *testing.B) {
	for _, config := range remediationBenchmarkCases {
		b.Run(config.name, func(b *testing.B) {
			b.StopTimer()
			fixture := newRemediationBenchmarkFixture(b, config)
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(config.plans), "plans/op")
			b.StartTimer()
			for index := 0; index < b.N; index++ {
				checked, err := compiler.CompileChecked(b.Context(), fixture.source, compiler.CompileOptions{})
				if err != nil || checked.PlanCount() != config.plans || checked.StepCount() != config.plans {
					b.Fatalf("checked compilation did not preserve workload: %v", err)
				}
			}
			b.StopTimer()
		})
	}
}

func BenchmarkRemediationIRChecking(b *testing.B) {
	for _, config := range remediationBenchmarkCases {
		b.Run(config.name, func(b *testing.B) {
			b.StopTimer()
			fixture := newRemediationBenchmarkFixture(b, config)
			artifact := fixture.checked.CloneArtifact()
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(config.plans), "plans/op")
			b.StartTimer()
			for index := 0; index < b.N; index++ {
				checked, err := ir.Check(artifact, fixture.environment, ir.Limits{})
				if err != nil || checked.Digest() != fixture.checked.Digest() {
					b.Fatalf("IR checking changed the workload: %v", err)
				}
			}
			b.StopTimer()
		})
	}
}

func BenchmarkRemediationDryRun(b *testing.B) {
	for _, config := range remediationBenchmarkCases {
		for _, match := range []struct {
			name string
			risk int
			want int
		}{{"none", 0, 0}, {"one", 1, 1}, {"all", config.plans, config.plans}} {
			b.Run(config.name+"/"+match.name, func(b *testing.B) {
				b.StopTimer()
				fixture := newRemediationBenchmarkFixture(b, config)
				fixture.facts["order.risk"] = int64(match.risk)
				engine, _, executor := newRemediationBenchmarkEngine(b, fixture)
				defer closeRemediationBenchmarkEngine(b, engine)
				b.ReportAllocs()
				b.ResetTimer()
				b.ReportMetric(float64(config.plans), "plans/op")
				b.StartTimer()
				for index := 0; index < b.N; index++ {
					evaluations, err := engine.DryRun(b.Context(), fixture.facts)
					if err != nil || len(evaluations) != config.plans {
						b.Fatalf("dry-run did not evaluate all plans: %v", err)
					}
					matched := 0
					for _, evaluation := range evaluations {
						if evaluation.Matched {
							matched++
						}
					}
					if matched != match.want {
						b.Fatalf("matched %d plans, want %d", matched, match.want)
					}
				}
				b.StopTimer()
				if executor.calls.Load() != 0 {
					b.Fatal("dry-run invoked an executor")
				}
			})
		}
	}
}

func BenchmarkRemediationAdmission(b *testing.B) {
	for _, config := range remediationBenchmarkCases {
		b.Run(config.name, func(b *testing.B) {
			b.StopTimer()
			fixture := newRemediationBenchmarkFixture(b, config)
			request := remediationBenchmarkRequest(fixture, "new-admission", WaitAccepted)
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(config.plans), "plans/op")
			for index := 0; index < b.N; index++ {
				engine, durable, executor := newRemediationBenchmarkEngine(b, fixture)
				b.StartTimer()
				accepted, err := engine.Execute(b.Context(), request)
				b.StopTimer()
				if err != nil || !accepted.DurablyAccepted || accepted.Completed || executor.calls.Load() != 0 {
					closeRemediationBenchmarkEngine(b, engine)
					b.Fatalf("expected nonterminal admission without invocation: %v", err)
				}
				record, lookupErr := durable.GetExecution(b.Context(), accepted.ExecutionID)
				if lookupErr != nil || len(record.Plans) != config.plans {
					closeRemediationBenchmarkEngine(b, engine)
					b.Fatalf("admission did not persist every matched plan: %v", lookupErr)
				}
				closeRemediationBenchmarkEngine(b, engine)
			}
		})
	}
}

func BenchmarkRemediationTerminalReplay(b *testing.B) {
	for _, config := range remediationBenchmarkCases {
		b.Run(config.name, func(b *testing.B) {
			b.StopTimer()
			fixture := newRemediationBenchmarkFixture(b, config)
			engine, _, executor := newRemediationBenchmarkEngine(b, fixture)
			defer closeRemediationBenchmarkEngine(b, engine)
			request := remediationBenchmarkRequest(fixture, "terminal-replay", WaitTerminal)
			first, err := engine.Execute(b.Context(), request)
			if err != nil || !first.Completed || executor.calls.Load() != int64(config.plans) {
				b.Fatalf("replay setup must complete the workload once: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(config.plans), "plans/execution")
			b.StartTimer()
			for index := 0; index < b.N; index++ {
				replayed, err := engine.Execute(b.Context(), request)
				if err != nil || replayed != first {
					b.Fatalf("terminal replay changed the result: %v", err)
				}
			}
			b.StopTimer()
			if executor.calls.Load() != int64(config.plans) {
				b.Fatal("terminal replay invoked an executor again")
			}
		})
	}
}

func BenchmarkRemediationRecovery(b *testing.B) {
	for _, batch := range []int{1, 8, 32} {
		b.Run(fmt.Sprintf("batch-%d", batch), func(b *testing.B) {
			b.StopTimer()
			fixture := newRemediationBenchmarkFixture(b, remediationBenchmarkCases[0])
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(batch), "executions/op")
			for index := 0; index < b.N; index++ {
				engine, durable, executor := newRemediationBenchmarkEngine(b, fixture)
				for item := 0; item < batch; item++ {
					accepted, err := engine.Execute(b.Context(), remediationBenchmarkRequest(fixture, fmt.Sprintf("recovery-%02d", item), WaitAccepted))
					if err != nil || !accepted.DurablyAccepted || accepted.Completed {
						closeRemediationBenchmarkEngine(b, engine)
						b.Fatalf("recovery setup did not admit work: %v", err)
					}
				}
				worker := &RecoveryWorker{Engine: engine, Store: durable, Owner: "benchmark-recovery", BatchSize: batch, LeaseDuration: time.Minute}
				b.StartTimer()
				processed, err := worker.RunOnce(b.Context())
				b.StopTimer()
				if err != nil || processed != batch || executor.calls.Load() != int64(batch) {
					closeRemediationBenchmarkEngine(b, engine)
					b.Fatalf("recovery did not process all work: processed=%d calls=%d err=%v", processed, executor.calls.Load(), err)
				}
				for item := 0; item < batch; item++ {
					record, err := durable.GetExecution(b.Context(), fmt.Sprintf("recovery-%02d", item))
					if err != nil || record.State != schema.ExecutionCompleted {
						closeRemediationBenchmarkEngine(b, engine)
						b.Fatalf("recovery left nonterminal work: %v", err)
					}
				}
				closeRemediationBenchmarkEngine(b, engine)
			}
		})
	}
}
