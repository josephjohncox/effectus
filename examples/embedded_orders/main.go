package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/josephjohncox/effectus/embedded"
	orderreview "github.com/josephjohncox/effectus/internal/demo/orderreview"
	"github.com/josephjohncox/effectus/invocation"
)

type reviewService struct {
	mu      sync.Mutex
	reviews map[string]map[string]any
}

func (service *reviewService) requestReview(_ context.Context, request invocation.Request) invocation.Outcome {
	key := request.Metadata.Saga.IdempotencyKey
	if key == "" {
		return embedded.Permanent(fmt.Errorf("review idempotency key is required"))
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if existing, ok := service.reviews[key]; ok {
		return embedded.Success(existing["review_id"])
	}
	review := map[string]any{
		"review_id": "review-" + request.Arguments["orderId"].(string),
		"order_id":  request.Arguments["orderId"],
		"reason":    request.Arguments["reason"],
		"status":    "pending",
	}
	service.reviews[key] = review
	return embedded.Success(review["review_id"])
}

func main() {
	ctx := context.Background()
	reviews := &reviewService{reviews: make(map[string]map[string]any)}
	ruleSource, err := orderreview.RuleSource()
	if err != nil {
		log.Fatal(err)
	}
	application, err := embedded.New("order-review", "1.0.0").
		AddFact("order.id", "").
		AddFact("order.total", 0.0).
		AddFact("order.currency", "").
		AddFact("order.risk_score", int64(0)).
		AddSource("order_review.eff", ruleSource).
		AddVerb(embedded.Verb{
			Name:         "RequestManualReview",
			Description:  "Create a manual review for a risky order",
			ArgTypes:     map[string]string{"orderId": "string", "reason": "string"},
			RequiredArgs: []string{"orderId", "reason"},
			ReturnType:   "string",
			Capabilities: []string{"write", "create", "idempotent"},
			Resources: []embedded.Resource{{
				Name: "order_review", Capabilities: []string{"write", "create", "idempotent"},
			}},
			Handler: reviews.requestReview,
		}).
		Build(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()

	scenario, err := orderreview.CanonicalScenario()
	if err != nil {
		log.Fatal(err)
	}
	request := embedded.Request{
		Namespace:      scenario.Request.Namespace,
		IdempotencyKey: scenario.IdempotencyKey,
		Facts:          scenario.Facts(),
	}
	first, err := application.Execute(ctx, request)
	if err != nil {
		log.Fatal(err)
	}
	second, err := application.Execute(ctx, request)
	if err != nil {
		log.Fatal(err)
	}

	reviewCount := len(reviews.reviews)
	if !first.Completed || !second.Completed {
		log.Fatalf("execution did not complete: first=%t replay=%t", first.Completed, second.Completed)
	}
	if first.ExecutionID == "" || first.ExecutionID != second.ExecutionID {
		log.Fatalf("replay execution ID mismatch: first=%q replay=%q", first.ExecutionID, second.ExecutionID)
	}
	if reviewCount != 1 {
		log.Fatalf("review count = %d, want 1", reviewCount)
	}

	output := map[string]any{
		"execution_id":       first.ExecutionID,
		"replayed_execution": second.ExecutionID,
		"completed":          first.Completed,
		"review_count":       reviewCount,
	}
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(encoded))
}
