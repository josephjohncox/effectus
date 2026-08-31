package kafka

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/josephjohncox/effectus/loader"
	effectusruntime "github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
	segmentio "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

type executeEngineFunc func(context.Context, effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error)

func (function executeEngineFunc) Execute(ctx context.Context, request effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error) {
	return function(ctx, request)
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

func TestCompletedKafkaRedeliveryExecutesEffectsExactlyOnce(t *testing.T) {
	var effects atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		effects.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer sink.Close()
	directory := t.TempDir()
	manifest := fmt.Sprintf(`{"name":"test","version":"1","verbs":[{"name":"charge","capabilities":["write"],"resources":[{"resource":"payment","capabilities":["write"]}],"argTypes":{"amount":"int"},"requiredArgs":["amount"],"returnType":"void","target":{"type":"http","config":{"url":%q,"method":"POST","timeout":"2s","allowPrivateNetwork":true}}}]}`, sink.URL)
	manifestPath := filepath.Join(directory, "extension.verbs.json")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifest), 0o600))
	execution := effectusruntime.NewExecutionRuntime()
	execution.RegisterExtensionLoader(loader.NewJSONVerbLoader("test", manifestPath))
	execution.RegisterExtensionLoader(loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(`flow "charge" priority 1 { when {} steps { charge(amount: 1) } }`)))
	require.NoError(t, execution.CompileAndValidate(t.Context()))
	require.NoError(t, execution.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "kafka-test"}))
	handler, err := NewEngineHandler(EngineHandlerConfig{Ruleset: "orders", Version: "1", WaitMode: effectusruntime.WaitTerminal}, execution.Engine())
	require.NoError(t, err)
	delivery := Delivery{ID: "kafka/cluster/facts/0/44", Message: segmentio.Message{Value: []byte(`{"facts":{"id":"42"}}`)}}
	first, err := handler.Handle(t.Context(), delivery)
	require.NoError(t, err)
	require.True(t, first.Completed)
	second, err := handler.Handle(t.Context(), delivery)
	require.NoError(t, err)
	require.True(t, second.Completed)
	require.Equal(t, int32(1), effects.Load())
}

func TestCompletedContractUsesTerminalWait(t *testing.T) {
	handler, err := NewEngineHandler(EngineHandlerConfig{
		Ruleset: "orders", Version: "1", WaitMode: effectusruntime.WaitTerminal,
	}, executeEngineFunc(func(_ context.Context, request effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error) {
		require.Equal(t, effectusruntime.WaitTerminal, request.WaitMode)
		return effectusruntime.ExecuteResult{DurablyAccepted: true, Completed: true}, nil
	}))
	require.NoError(t, err)
	result, err := handler.Handle(t.Context(), Delivery{ID: "delivery", Message: segmentio.Message{Value: []byte(`{"facts":{"id":1}}`)}})
	require.NoError(t, err)
	require.True(t, result.Completed)
}
