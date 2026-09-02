package kafka

import (
	"context"
	"testing"

	effectusruntime "github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
	segmentio "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

type executeEngineFunc func(context.Context, effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error)

func (function executeEngineFunc) Execute(ctx context.Context, request effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error) {
	return function(ctx, request)
}

func TestAckContractMapsToEngineBoundary(t *testing.T) {
	for contract, want := range map[AckContract]effectusruntime.WaitMode{
		AckAfterDurableAcceptance:   effectusruntime.WaitAccepted,
		AckAfterCompletedProcessing: effectusruntime.WaitTerminal,
	} {
		got, err := WaitModeForAckContract(contract)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err := WaitModeForAckContract("unknown")
	require.Error(t, err)
}

func TestEngineHandlerUsesStableExecutionIdentity(t *testing.T) {
	var captured effectusruntime.ExecuteRequest
	handler, err := NewEngineHandler(EngineHandlerConfig{
		Ruleset: "orders", Version: "1.0.0", DefaultTenant: "default", WaitMode: effectusruntime.WaitAccepted,
	}, executeEngineFunc(func(_ context.Context, request effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error) {
		captured = request
		return effectusruntime.ExecuteResult{ExecutionID: request.Admission.ExecutionID, DurablyAccepted: true}, nil
	}))
	require.NoError(t, err)
	delivery := Delivery{
		ID:      "kafka/cluster/facts/0/9",
		Message: segmentio.Message{Value: []byte(`{"namespace":"tenant-a","facts":{"order_id":"42"}}`)},
	}
	result, err := handler.Handle(t.Context(), delivery)
	require.NoError(t, err)
	require.True(t, result.DurablyAccepted)
	require.Equal(t, effectusruntime.WaitAccepted, captured.WaitMode)
	require.Equal(t, delivery.ID, captured.Admission.AdmissionID)
	require.Equal(t, schema.StableExecutionID("tenant-a", delivery.ID, "orders", "1.0.0"), captured.Admission.ExecutionID)
}

func TestDurableContractCommitsAfterEngineAcceptance(t *testing.T) {
	source, _, committer := testSource(t, func(config *Config) {
		config.AckContract = AckAfterDurableAcceptance
		config.MaxAttempts = 1
	}, segmentio.Message{
		Topic: "facts", Partition: 0, Offset: 4,
		Value: []byte(`{"namespace":"tenant-a","facts":{"order_id":"42"}}`),
	})
	handler, err := NewEngineHandler(EngineHandlerConfig{
		Ruleset: "orders", Version: "1.0.0", WaitMode: effectusruntime.WaitAccepted,
	}, executeEngineFunc(func(_ context.Context, request effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error) {
		require.Equal(t, effectusruntime.WaitAccepted, request.WaitMode)
		return effectusruntime.ExecuteResult{DurablyAccepted: true}, nil
	}))
	require.NoError(t, err)
	require.NoError(t, source.Run(t.Context(), handler))
	require.Equal(t, 1, committer.count())
}

func TestDurableContractDoesNotCommitWhenEngineHasNotCommittedAdmission(t *testing.T) {
	source, _, committer := testSource(t, func(config *Config) { config.AckContract = AckAfterDurableAcceptance; config.MaxAttempts = 1 }, segmentio.Message{Topic: "facts", Partition: 0, Offset: 5, Value: []byte(`{"facts":{"id":1}}`)})
	handler, err := NewEngineHandler(EngineHandlerConfig{Ruleset: "orders", Version: "1", WaitMode: effectusruntime.WaitAccepted}, executeEngineFunc(func(context.Context, effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error) {
		return effectusruntime.ExecuteResult{DurablyAccepted: false}, nil
	}))
	require.NoError(t, err)
	err = source.Run(t.Context(), handler)
	require.ErrorIs(t, err, ErrPoisonMessage)
	require.Zero(t, committer.count())
}

func TestCompletedContractCommitsOnlyAfterTerminalEngineResult(t *testing.T) {
	source, _, committer := testSource(t, func(config *Config) {
		config.AckContract = AckAfterCompletedProcessing
		config.MaxAttempts = 1
	}, segmentio.Message{Topic: "facts", Partition: 0, Offset: 6, Value: []byte(`{"facts":{"id":1}}`)})
	handler, err := NewEngineHandler(EngineHandlerConfig{
		Ruleset: "orders", Version: "1", WaitMode: effectusruntime.WaitTerminal,
	}, executeEngineFunc(func(_ context.Context, request effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error) {
		require.Equal(t, effectusruntime.WaitTerminal, request.WaitMode)
		return effectusruntime.ExecuteResult{DurablyAccepted: true, Completed: true}, nil
	}))
	require.NoError(t, err)
	require.NoError(t, source.Run(t.Context(), handler))
	require.Equal(t, 1, committer.count())
}
