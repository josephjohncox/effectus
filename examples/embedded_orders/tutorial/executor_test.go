package main

import (
	"context"
	"sync"
	"testing"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func tutorialInvocation(t *testing.T, verb, key string, args map[string]any) invocation.Request {
	t.Helper()
	_, hash, err := schema.CanonicalJSON(args)
	require.NoError(t, err)
	return invocation.Request{Verb: verb, ArgumentHash: hash, ContractHash: "tutorial-contract", Arguments: args, Metadata: invocation.Context{Saga: invocation.Saga{IdempotencyKey: key}}}
}

func TestTutorialBusinessCommitAndDeduplicationShareALock(t *testing.T) {
	executor := &tutorialExecutor{}
	producer := tutorialInvocation(t, "RequestManualReview", "produce", map[string]any{"orderId": "order-200", "reason": "value_or_risk"})
	consumer := tutorialInvocation(t, "RecordReview", "consume", map[string]any{"orderId": "order-200", "ticket": "ticket:order-200"})
	for _, request := range []invocation.Request{producer, consumer} {
		outcomes := make(chan invocation.Outcome, 16)
		var joined sync.WaitGroup
		for i := 0; i < 16; i++ {
			joined.Add(1)
			go func() { defer joined.Done(); outcomes <- executor.Invoke(t.Context(), request) }()
		}
		joined.Wait()
		close(outcomes)
		for outcome := range outcomes {
			require.Equal(t, invocation.OutcomeSuccess, outcome.Class)
			require.NoError(t, outcome.Err)
		}
	}
	require.Len(t, executor.snapshot(), 2)
	changed := producer
	changed.ContractHash = "changed-contract"
	require.Equal(t, invocation.OutcomePermanentFailure, executor.Invoke(t.Context(), changed).Class)
	changed = tutorialInvocation(t, "RequestManualReview", "produce", map[string]any{"orderId": "other-order"})
	require.Equal(t, invocation.OutcomePermanentFailure, executor.Invoke(t.Context(), changed).Class)
	require.Len(t, executor.snapshot(), 2)
}

func TestTutorialCannotRecordBeforeReviewOrAfterCanceledAdmission(t *testing.T) {
	executor := &tutorialExecutor{}
	for _, ticket := range []string{"", "ticket:order-200"} {
		request := tutorialInvocation(t, "RecordReview", "consume", map[string]any{"orderId": "order-200", "ticket": ticket})
		require.Equal(t, invocation.OutcomePermanentFailure, executor.Invoke(t.Context(), request).Class)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := tutorialInvocation(t, "RequestManualReview", "produce", map[string]any{"orderId": "order-200"})
	require.Equal(t, invocation.OutcomeRetryableKnownNotCommitted, executor.Invoke(ctx, request).Class)
	require.Empty(t, executor.snapshot())
}
