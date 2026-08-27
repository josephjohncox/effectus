package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	kafkaadapter "github.com/effectus/effectus-go/adapters/kafka"
	effectusruntime "github.com/effectus/effectus-go/runtime"
	segmentio "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

func TestDecodeKafkaFactEnvelopeRejectsUnknownAndReservedFields(t *testing.T) {
	_, err := decodeKafkaFactEnvelope([]byte(`{"facts":{"ready":true},"execution_id":"spoof"}`))
	require.ErrorContains(t, err, "unknown field")
	_, err = decodeKafkaFactEnvelope([]byte(`{"facts":{"ready":true}} {}`))
	require.Error(t, err)
	_, err = decodeKafkaFactEnvelope([]byte(`{"universe":"default"}`))
	require.ErrorContains(t, err, "facts are required")
}

type daemonExecuteEngineFunc func(context.Context, effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error)

func (function daemonExecuteEngineFunc) Execute(ctx context.Context, request effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error) {
	return function(ctx, request)
}

func TestKafkaFactHandlerDelegatesToSharedEngineWithStableIdentity(t *testing.T) {
	var captured effectusruntime.ExecuteRequest
	delegate, err := kafkaadapter.NewEngineHandler(kafkaadapter.EngineHandlerConfig{Ruleset: "orders", Version: "1.0.0", WaitMode: effectusruntime.WaitAccepted}, daemonExecuteEngineFunc(func(_ context.Context, request effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error) {
		captured = request
		return effectusruntime.ExecuteResult{DurablyAccepted: true}, nil
	}))
	require.NoError(t, err)
	delivery := kafkaadapter.Delivery{ID: "kafka/cluster/facts/0/12", Message: segmentio.Message{Value: []byte(`{"universe":"tenant-a","namespace":"tenant-a","facts":{"ready":true}}`)}, Attempt: 1}
	result, err := (kafkaFactHandler{delegate: delegate}).Handle(t.Context(), delivery)
	require.NoError(t, err)
	require.True(t, result.DurablyAccepted)
	require.Equal(t, delivery.ID, captured.Admission.AdmissionID)
	require.Equal(t, "tenant-a", captured.Admission.TenantNamespace)
}

func TestKafkaDeliveryLedgerPersistsAttemptsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deliveries.jsonl")
	first := &filePoisonAcknowledger{path: path}
	attempt, err := first.RecordFailure(t.Context(), "delivery")
	require.NoError(t, err)
	require.Equal(t, 1, attempt)
	second := &filePoisonAcknowledger{path: path}
	attempt, err = second.RecordFailure(t.Context(), "delivery")
	require.NoError(t, err)
	require.Equal(t, 2, attempt)
	require.NoError(t, second.ClearAttempts(t.Context(), "delivery"))
	third := &filePoisonAcknowledger{path: path}
	attempt, err = third.Attempts(t.Context(), "delivery")
	require.NoError(t, err)
	require.Zero(t, attempt)
}

func TestKafkaDeliveryLedgerClearsCommittedStateFromMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deliveries.jsonl")
	ledger := &filePoisonAcknowledger{path: path}
	_, err := ledger.RecordFailure(t.Context(), "delivery")
	require.NoError(t, err)
	require.NoError(t, ledger.AcknowledgePoison(t.Context(), kafkaadapter.PoisonDisposition{DeliveryID: "delivery", Policy: kafkaadapter.PoisonSkip}))
	require.NoError(t, ledger.ClearAttempts(t.Context(), "delivery"))
	require.Empty(t, ledger.attempts)
	require.Empty(t, ledger.acknowledged)
	reloaded := &filePoisonAcknowledger{path: path}
	attempts, err := reloaded.Attempts(t.Context(), "delivery")
	require.NoError(t, err)
	require.Zero(t, attempts)
	acknowledged, err := reloaded.PoisonAcknowledged(t.Context(), "delivery")
	require.NoError(t, err)
	require.False(t, acknowledged)
}

func TestFilePoisonAcknowledgerPersistsDisposition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "poison.jsonl")
	acknowledger := &filePoisonAcknowledger{path: path}
	require.NoError(t, acknowledger.AcknowledgePoison(context.Background(), kafkaadapter.PoisonDisposition{
		DeliveryID: "kafka/cluster/facts/0/1", Policy: kafkaadapter.PoisonSkip,
		Attempts: 3, Error: "invalid", Message: segmentio.Message{Topic: "facts", Partition: 0, Offset: 1},
	}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), `"delivery_id":"kafka/cluster/facts/0/1"`)
	require.Contains(t, string(data), `"policy":"skip"`)
}
