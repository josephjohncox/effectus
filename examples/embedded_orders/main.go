package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/embedded"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
)

const resolverID = "example/embedded-orders/v1"

type reviewExecutor struct{ reviews *int }

func (executor reviewExecutor) Invoke(_ context.Context, _ invocation.Request) invocation.Outcome {
	(*executor.reviews)++
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
}

func main() {
	ctx := context.Background()
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorEmbedded, ResolverID: resolverID, Reference: "order-review"})
	if err != nil {
		fail(err)
	}
	var scenario struct {
		IdempotencyKey string `json:"idempotency_key"`
		Request        struct {
			Namespace string         `json:"namespace"`
			Facts     map[string]any `json:"facts"`
		} `json:"request"`
	}
	rule, scenarioJSON, err := sharedOrderReviewArtifacts()
	if err != nil {
		fail(err)
	}
	if err := json.Unmarshal(scenarioJSON, &scenario); err != nil {
		fail(fmt.Errorf("decode shared order-review scenario: %w", err))
	}
	source, err := bundle.New(bundle.Spec{
		Name: "order-review", Version: "1.0.0",
		Sources: []bundle.Source{{Path: "rules/order_review.eff", Content: string(rule)}},

		Environment: ir.Environment{
			Facts: map[string]string{"order.id": "string", "order.total": "float", "order.risk_score": "int"},
			Verbs: map[string]ir.VerbContract{"RequestManualReview": {Arguments: map[string]string{"orderId": "string", "reason": "string"}, RequiredArgs: []string{"orderId", "reason"}, ResultType: "bool"}},
		},
		Executors: map[string]invocation.Descriptor{"RequestManualReview": descriptor},
	})
	if err != nil {
		fail(err)
	}
	var reviews int
	resolvers, err := invocation.NewRegistry([]invocation.ResolverRegistration{{ID: resolverID, Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
		return reviewExecutor{reviews: &reviews}, nil, nil
	})}})
	if err != nil {
		fail(err)
	}
	runtime, err := embedded.Open(ctx, source, resolvers)
	if err != nil {
		fail(err)
	}
	defer runtime.Close()
	request := embedded.Request{Namespace: scenario.Request.Namespace, IdempotencyKey: scenario.IdempotencyKey, Facts: scenario.Request.Facts}
	first, err := runtime.Execute(ctx, request)
	if err != nil {
		fail(err)
	}
	second, err := runtime.Execute(ctx, request)
	if err != nil {
		fail(err)
	}
	fmt.Fprintln(os.Stderr, "Runtime compiled successfully with 1 verbs, 0 functions")
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"completed": first.Completed, "execution_id": first.ExecutionID, "replayed_execution": second.ExecutionID, "review_count": reviews}); err != nil {
		fail(err)
	}
}

// sharedOrderReviewArtifacts uses the source location, not the process working
// directory, so `go run ./examples/embedded_orders` reads the one shared demo.
func sharedOrderReviewArtifacts() ([]byte, []byte, error) {
	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		return nil, nil, fmt.Errorf("resolve embedded example source path")
	}
	root := filepath.Join(filepath.Dir(file), "..", "order_review")
	rule, err := os.ReadFile(filepath.Join(root, "rules", "order_review.eff"))
	if err != nil {
		return nil, nil, fmt.Errorf("read shared order-review rule: %w", err)
	}
	scenario, err := os.ReadFile(filepath.Join(root, "data", "order.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("read shared order-review scenario: %w", err)
	}
	return rule, scenario, nil
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
