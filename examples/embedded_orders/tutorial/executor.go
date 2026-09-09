package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/josephjohncox/effectus/invocation"
)

type operation struct {
	Verb   string `json:"verb"`
	Ticket string `json:"ticket"`
}

type rememberedCall struct {
	argumentHash string
	contractHash string
	verb         string
	result       any
}

// tutorialExecutor combines business state and deduplication under one mutex.
// Everything is process-local. This is not a durable destination implementation.
type tutorialExecutor struct {
	mu         sync.Mutex
	calls      map[string]rememberedCall
	tickets    map[string]string
	operations []operation
}

func (executor *tutorialExecutor) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	if err := ctx.Err(); err != nil {
		return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: err}
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	key := request.Metadata.Saga.IdempotencyKey
	if key == "" {
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: fmt.Errorf("business idempotency key is required")}
	}
	if prior, exists := executor.calls[key]; exists {
		if prior.argumentHash != request.ArgumentHash || prior.contractHash != request.ContractHash || prior.verb != request.Verb {
			return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: fmt.Errorf("business identity conflicts with prior arguments")}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: prior.result}
	}
	orderID, ok := request.Arguments["orderId"].(string)
	if !ok || orderID == "" {
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: fmt.Errorf("orderId must be a nonblank string")}
	}
	var result any
	var ticket string
	switch request.Verb {
	case "RequestManualReview":
		ticket = "ticket:" + orderID
		result = ticket
	case "RecordReview":
		ticket, ok = request.Arguments["ticket"].(string)
		if !ok || ticket == "" || executor.tickets[orderID] != ticket {
			return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: fmt.Errorf("record requires the earlier review ticket")}
		}
	default:
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: fmt.Errorf("unsupported tutorial verb %q", request.Verb)}
	}
	// Recheck cancellation before the in-memory business commit. Cancellation
	// after this point does not change the observed success into a safe retry.
	if err := ctx.Err(); err != nil {
		return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: err}
	}
	if executor.calls == nil {
		executor.calls = make(map[string]rememberedCall)
		executor.tickets = make(map[string]string)
	}
	executor.tickets[orderID] = ticket
	executor.calls[key] = rememberedCall{argumentHash: request.ArgumentHash, contractHash: request.ContractHash, verb: request.Verb, result: result}
	executor.operations = append(executor.operations, operation{Verb: request.Verb, Ticket: ticket})
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: result}
}

func (executor *tutorialExecutor) snapshot() []operation {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return append([]operation{}, executor.operations...)
}
