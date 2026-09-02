//go:build integration

package kafka

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	segmentio "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

func TestKafkaConsumerGroupCommitAndRestart(t *testing.T) {
	brokersValue := strings.TrimSpace(os.Getenv("KAFKA_BROKERS"))
	if brokersValue == "" {
		t.Skip("KAFKA_BROKERS is required for Kafka integration tests")
	}
	brokers := strings.Split(brokersValue, ",")
	topic := "effectus-integration-" + uuid.NewString()
	group := "effectus-integration-" + uuid.NewString()
	writer := &segmentio.Writer{
		Addr: segmentio.TCP(brokers...), Topic: topic, Balancer: &segmentio.LeastBytes{},
		RequiredAcks: segmentio.RequireAll, AllowAutoTopicCreation: true,
	}
	t.Cleanup(func() { _ = writer.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, writer.WriteMessages(ctx,
		segmentio.Message{Key: []byte("one"), Value: []byte(`{"facts":{"n":1}}`)},
		segmentio.Message{Key: []byte("two"), Value: []byte(`{"facts":{"n":2}}`)},
	))

	newSource := func() *KafkaSource {
		source, err := NewKafkaSource(&Config{
			SourceID: "integration", ClusterNamespace: "integration", Brokers: brokers,
			Topic: topic, ConsumerGroup: group, StartOffset: "earliest",
			AckContract: AckAfterCompletedProcessing, MaxAttempts: 1, PoisonPolicy: PoisonHalt,
		})
		require.NoError(t, err)
		require.NoError(t, source.SetAttemptTracker(NewMemoryAttemptTracker()))
		return source
	}

	first := newSource()
	firstContext, stopFirst := context.WithCancel(ctx)
	var handled atomic.Int32
	require.NoError(t, first.Run(firstContext, HandlerFunc(func(_ context.Context, _ Delivery) (HandleResult, error) {
		if handled.Add(1) == 2 {
			stopFirst()
		}
		return HandleResult{Completed: true}, nil
	})))
	require.Equal(t, int32(2), handled.Load())

	second := newSource()
	secondContext, stopSecond := context.WithTimeout(ctx, 2*time.Second)
	defer stopSecond()
	var replayed atomic.Int32
	require.NoError(t, second.Run(secondContext, HandlerFunc(func(_ context.Context, _ Delivery) (HandleResult, error) {
		replayed.Add(1)
		return HandleResult{Completed: true}, nil
	})))
	require.Zero(t, replayed.Load(), "committed records must not replay after consumer restart")
}
