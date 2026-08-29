package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/effectus/effectus-go/embedded"
	"github.com/effectus/effectus-go/invocation"
)

//go:embed rules/order_review.eff
var orderRules []byte

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
	application, err := embedded.New("order-review", "1.0.0").
		AddFact("order.id", "").
		AddFact("order.total", 0.0).
		AddFact("order.risk_score", int64(0)).
		AddSource("order_review.eff", orderRules).
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

	request := embedded.Request{
		Namespace:      "merchant-42",
		IdempotencyKey: "order-100-created",
		Facts: map[string]any{
			"order": map[string]any{
				"id": "order-100", "total": 2499.00, "risk_score": int64(82),
			},
		},
	}
	first, err := application.Execute(ctx, request)
	if err != nil {
		log.Fatal(err)
	}
	second, err := application.Execute(ctx, request)
	if err != nil {
		log.Fatal(err)
	}

	output := map[string]any{
		"execution_id":       first.ExecutionID,
		"replayed_execution": second.ExecutionID,
		"completed":          first.Completed,
		"review_count":       len(reviews.reviews),
	}
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(encoded))
}
