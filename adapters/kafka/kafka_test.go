package kafka

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	segmentio "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

type fakeConsumer struct {
	messages  []segmentio.Message
	committer *fakeCommitter
	mu        sync.Mutex
	fetches   int
	closed    bool
}

func (consumer *fakeConsumer) Run(ctx context.Context, process func(context.Context, segmentio.Message, recordCommitter) error) error {
	for _, message := range consumer.messages {
		if err := ctx.Err(); err != nil {
			return nil
		}
		consumer.mu.Lock()
		consumer.fetches++
		consumer.mu.Unlock()
		if err := process(ctx, cloneMessage(message), consumer.committer); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
	return nil
}

func (consumer *fakeConsumer) Close() error {
	consumer.mu.Lock()
	consumer.closed = true
	consumer.mu.Unlock()
	return nil
}

func (consumer *fakeConsumer) fetchCount() int {
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	return consumer.fetches
}

type fakeCommitter struct {
	mu       sync.Mutex
	messages []segmentio.Message
	err      error
	onCommit func(segmentio.Message)
}

func (committer *fakeCommitter) Commit(ctx context.Context, message segmentio.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	committer.mu.Lock()
	defer committer.mu.Unlock()
	if committer.err != nil {
		return committer.err
	}
	if committer.onCommit != nil {
		committer.onCommit(message)
	}
	committer.messages = append(committer.messages, cloneMessage(message))
	return nil
}

func (committer *fakeCommitter) count() int {
	committer.mu.Lock()
	defer committer.mu.Unlock()
	return len(committer.messages)
}

type fakePublisher struct {
	mu        sync.Mutex
	messages  []segmentio.Message
	err       error
	onPublish func(segmentio.Message)
}

func (publisher *fakePublisher) WriteMessages(ctx context.Context, messages ...segmentio.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if publisher.err != nil {
		return publisher.err
	}
	for _, message := range messages {
		if publisher.onPublish != nil {
			publisher.onPublish(message)
		}
		publisher.messages = append(publisher.messages, cloneMessage(message))
	}
	return nil
}

type fakePoisonAck struct {
	mu           sync.Mutex
	dispositions []PoisonDisposition
	err          error
	onAck        func(PoisonDisposition)
}

func (ack *fakePoisonAck) AcknowledgePoison(ctx context.Context, disposition PoisonDisposition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.err != nil {
		return ack.err
	}
	if ack.onAck != nil {
		ack.onAck(disposition)
	}
	ack.dispositions = append(ack.dispositions, disposition)
	return nil
}

func testSource(t *testing.T, mutate func(*Config), messages ...segmentio.Message) (*KafkaSource, *fakeConsumer, *fakeCommitter) {
	t.Helper()
	config := &Config{
		SourceID: "source", ClusterNamespace: "cluster", Brokers: []string{"broker:9092"},
		Topic: "facts", ConsumerGroup: "effectus", AckContract: AckAfterCompletedProcessing,
		MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
		PoisonPolicy: PoisonHalt,
	}
	if mutate != nil {
		mutate(config)
	}
	resolved, err := normalizeConfig(config)
	require.NoError(t, err)
	committer := &fakeCommitter{}
	consumer := &fakeConsumer{messages: messages, committer: committer}
	return newKafkaSource(resolved, consumer, nil, nil), consumer, committer
}

func kafkaMessage(offset int64) segmentio.Message {
	return segmentio.Message{Topic: "facts", Partition: 2, Offset: offset, Key: []byte("key"), Value: []byte(`{"facts":{"ready":true}}`)}
}

func headerValue(headers []segmentio.Header, name string) string {
	for _, header := range headers {
		if header.Key == name {
			return string(header.Value)
		}
	}
	return ""
}

func runUntilMessagesConsumed(t *testing.T, source *KafkaSource, handler Handler) error {
	t.Helper()
	return source.Run(t.Context(), handler)
}

func TestRunCommitsOnlyAfterCompletedProcessing(t *testing.T) {
	source, _, committer := testSource(t, nil, kafkaMessage(10))
	var trace []string
	committer.onCommit = func(segmentio.Message) { trace = append(trace, "commit") }
	err := runUntilMessagesConsumed(t, source, HandlerFunc(func(_ context.Context, delivery Delivery) (HandleResult, error) {
		trace = append(trace, "handle")
		require.Equal(t, "kafka/cluster/facts/2/10", delivery.ID)
		return HandleResult{Completed: true}, nil
	}))
	require.NoError(t, err)
	require.Equal(t, []string{"handle", "commit"}, trace)
	require.Equal(t, 1, committer.count())
}

func TestRunDurableContractRejectsEarlyHandlerReturn(t *testing.T) {
	source, _, committer := testSource(t, func(config *Config) {
		config.AckContract = AckAfterDurableAcceptance
		config.MaxAttempts = 1
	}, kafkaMessage(1))
	err := source.Run(t.Context(), HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
		return HandleResult{Completed: true}, nil
	}))
	require.ErrorIs(t, err, ErrPoisonMessage)
	require.Zero(t, committer.count())
}

func TestRunRetriesSameRecordBeforeFetchingLaterRecord(t *testing.T) {
	source, consumer, committer := testSource(t, nil, kafkaMessage(1), kafkaMessage(2))
	var deliveries []Delivery
	err := source.Run(t.Context(), HandlerFunc(func(_ context.Context, delivery Delivery) (HandleResult, error) {
		deliveries = append(deliveries, delivery)
		if delivery.Message.Offset == 1 && delivery.Attempt < 3 {
			require.Equal(t, 1, consumer.fetchCount())
			return HandleResult{}, errors.New("temporary")
		}
		return HandleResult{Completed: true}, nil
	}))
	require.NoError(t, err)
	require.Equal(t, []int64{1, 1, 1, 2}, []int64{
		deliveries[0].Message.Offset, deliveries[1].Message.Offset,
		deliveries[2].Message.Offset, deliveries[3].Message.Offset,
	})
	require.Equal(t, []int{1, 2, 3, 1}, []int{
		deliveries[0].Attempt, deliveries[1].Attempt, deliveries[2].Attempt, deliveries[3].Attempt,
	})
	require.Equal(t, 2, committer.count())
}

func TestBackpressureBlocksBeforeFetchingAnotherRecord(t *testing.T) {
	source, consumer, committer := testSource(t, nil, kafkaMessage(1), kafkaMessage(2))
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- source.Run(t.Context(), HandlerFunc(func(_ context.Context, delivery Delivery) (HandleResult, error) {
			if delivery.Message.Offset == 1 {
				close(entered)
				<-release
			}
			return HandleResult{Completed: true}, nil
		}))
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first handler did not start")
	}
	require.Equal(t, 1, consumer.fetchCount())
	require.Zero(t, committer.count())
	close(release)
	require.NoError(t, <-done)
	require.Equal(t, 2, consumer.fetchCount())
	require.Equal(t, 2, committer.count())
}

func TestRebalanceCancellationDoesNotConsumePoisonAttempt(t *testing.T) {
	message := kafkaMessage(1)
	tracker := newMemoryAttemptTracker()
	first, _, _ := testSource(t, func(config *Config) { config.MaxAttempts = 1 }, message)
	require.NoError(t, first.SetAttemptTracker(tracker))
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, first.Run(ctx, HandlerFunc(func(handlerContext context.Context, _ Delivery) (HandleResult, error) {
		cancel()
		<-handlerContext.Done()
		return HandleResult{}, handlerContext.Err()
	})))
	failures, err := tracker.Attempts(t.Context(), DeliveryID("cluster", message))
	require.NoError(t, err)
	require.Zero(t, failures)
	second, _, committer := testSource(t, func(config *Config) { config.MaxAttempts = 1 }, message)
	require.NoError(t, second.SetAttemptTracker(tracker))
	require.NoError(t, second.Run(t.Context(), HandlerFunc(func(context.Context, Delivery) (HandleResult, error) { return HandleResult{Completed: true}, nil })))
	require.Equal(t, 1, committer.count())
}

func TestCommitOutageRedeliveryDoesNotConsumePoisonAttempt(t *testing.T) {
	message := kafkaMessage(1)
	tracker := newMemoryAttemptTracker()
	first, _, firstCommitter := testSource(t, func(config *Config) { config.MaxAttempts = 1 }, message)
	firstCommitter.err = errors.New("coordinator unavailable")
	require.NoError(t, first.SetAttemptTracker(tracker))
	var calls int
	handler := HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
		calls++
		return HandleResult{Completed: true}, nil
	})
	require.ErrorContains(t, first.Run(t.Context(), handler), "coordinator unavailable")
	failures, err := tracker.Attempts(t.Context(), DeliveryID("cluster", message))
	require.NoError(t, err)
	require.Zero(t, failures)

	second, _, secondCommitter := testSource(t, func(config *Config) { config.MaxAttempts = 1 }, message)
	require.NoError(t, second.SetAttemptTracker(tracker))
	require.NoError(t, second.Run(t.Context(), handler))
	require.Equal(t, 2, calls)
	require.Equal(t, 1, secondCommitter.count())
}

func TestKillBeforeCommitCompletedRedeliveryHasZeroRepeatedEffects(t *testing.T) {
	message := kafkaMessage(1)
	var effects int
	completed := false
	handler := HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
		if !completed {
			effects++
			completed = true
		}
		return HandleResult{DurablyAccepted: true, Completed: true}, nil
	})
	first, _, firstCommitter := testSource(t, func(config *Config) { config.AckContract = AckAfterDurableAcceptance }, message)
	firstCommitter.err = errors.New("commit outage")
	require.Error(t, first.Run(t.Context(), handler))
	second, _, committer := testSource(t, func(config *Config) { config.AckContract = AckAfterDurableAcceptance }, message)
	require.NoError(t, second.Run(t.Context(), handler))
	require.Equal(t, 1, effects)
	require.Equal(t, 1, committer.count())
}

func TestCommitFailurePreventsLaterFetch(t *testing.T) {
	source, consumer, committer := testSource(t, nil, kafkaMessage(1), kafkaMessage(2))
	committer.err = errors.New("coordinator unavailable")
	err := source.Run(t.Context(), HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
		return HandleResult{Completed: true}, nil
	}))
	require.ErrorContains(t, err, "coordinator unavailable")
	require.Equal(t, 1, consumer.fetchCount())
	require.Zero(t, committer.count())
}

func TestDefaultPoisonPolicyHaltsAndMarksUnhealthy(t *testing.T) {
	source, consumer, committer := testSource(t, func(config *Config) { config.MaxAttempts = 2 }, kafkaMessage(1), kafkaMessage(2))
	err := source.Run(t.Context(), HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
		return HandleResult{}, errors.New("invalid payload")
	}))
	require.ErrorIs(t, err, ErrPoisonMessage)
	require.Equal(t, 1, consumer.fetchCount())
	require.Zero(t, committer.count())
	require.ErrorIs(t, source.HealthCheck(), ErrPoisonMessage)
}

func TestSkipRequiresDurableAuditBeforeCommit(t *testing.T) {
	source, _, committer := testSource(t, func(config *Config) {
		config.PoisonPolicy = PoisonSkip
		config.MaxAttempts = 1
	}, kafkaMessage(1))
	err := source.Run(t.Context(), HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
		return HandleResult{}, errors.New("poison")
	}))
	require.ErrorContains(t, err, "requires a durable poison acknowledger")
	require.Zero(t, committer.count())

	var trace []string
	ack := &fakePoisonAck{onAck: func(PoisonDisposition) { trace = append(trace, "audit") }}
	require.NoError(t, source.SetPoisonAcknowledger(ack))
	committer.onCommit = func(segmentio.Message) { trace = append(trace, "commit") }
	err = runUntilMessagesConsumed(t, source, HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
		return HandleResult{}, errors.New("poison")
	}))
	require.NoError(t, err)
	require.Equal(t, []string{"audit", "commit"}, trace)
	require.Len(t, ack.dispositions, 1)
}

func TestPoisonDispositionCommitsBeforeLaterPartitionRecord(t *testing.T) {
	first := kafkaMessage(1)
	first.Partition = 1
	second := kafkaMessage(2)
	second.Partition = 0
	source, consumer, committer := testSource(t, func(config *Config) { config.PoisonPolicy = PoisonSkip; config.MaxAttempts = 1 }, first, second)
	var trace []string
	ack := &fakePoisonAck{onAck: func(disposition PoisonDisposition) {
		trace = append(trace, fmt.Sprintf("audit-%d", disposition.Message.Offset))
	}}
	require.NoError(t, source.SetPoisonAcknowledger(ack))
	committer.onCommit = func(message segmentio.Message) { trace = append(trace, fmt.Sprintf("commit-%d", message.Offset)) }
	require.NoError(t, source.Run(t.Context(), HandlerFunc(func(_ context.Context, delivery Delivery) (HandleResult, error) {
		trace = append(trace, fmt.Sprintf("handle-%d", delivery.Message.Offset))
		if delivery.Message.Offset == 1 {
			return HandleResult{}, errors.New("poison")
		}
		return HandleResult{Completed: true}, nil
	})))
	require.Equal(t, []string{"handle-1", "audit-1", "commit-1", "handle-2", "commit-2"}, trace)
	require.Equal(t, 2, consumer.fetchCount())
}

func TestDLQRequiresExplicitNonTransactionalDeliveryMode(t *testing.T) {
	_, err := normalizeConfig(&Config{SourceID: "source", Brokers: []string{"broker:9092"}, Topic: "facts", ConsumerGroup: "group", PoisonPolicy: PoisonDLQ, DLQTopic: "facts.dlq"})
	require.ErrorContains(t, err, "atomic DLQ publication and source-offset commit are not enabled")
}

func TestDLQPublicationIsAcknowledgedBeforeOriginalCommit(t *testing.T) {
	source, _, committer := testSource(t, func(config *Config) {
		config.PoisonPolicy = PoisonDLQ
		config.DLQTopic = "facts.dlq"
		config.DLQDeliveryMode = DLQAtLeastOnceNonTransactional
		config.MaxAttempts = 1
	}, kafkaMessage(7))
	var trace []string
	publisher := &fakePublisher{onPublish: func(segmentio.Message) { trace = append(trace, "publish") }}
	require.NoError(t, source.SetDLQPublisher(publisher))
	committer.onCommit = func(segmentio.Message) { trace = append(trace, "commit") }
	err := runUntilMessagesConsumed(t, source, HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
		return HandleResult{}, errors.New("poison")
	}))
	require.NoError(t, err)
	require.Equal(t, []string{"publish", "commit"}, trace)
	require.Len(t, publisher.messages, 1)
	require.Equal(t, "facts.dlq", publisher.messages[0].Topic)
	require.Equal(t, []byte("kafka/cluster/facts/2/7/dlq"), publisher.messages[0].Key)
	require.Equal(t, "kafka/cluster/facts/2/7/dlq", headerValue(publisher.messages[0].Headers, "effectus-dlq-id"))
}

func TestDLQFailureLeavesOriginalUncommitted(t *testing.T) {
	source, consumer, committer := testSource(t, func(config *Config) {
		config.PoisonPolicy = PoisonDLQ
		config.DLQTopic = "facts.dlq"
		config.DLQDeliveryMode = DLQAtLeastOnceNonTransactional
		config.MaxAttempts = 1
	}, kafkaMessage(1), kafkaMessage(2))
	require.NoError(t, source.SetDLQPublisher(&fakePublisher{err: errors.New("DLQ unavailable")}))
	err := source.Run(t.Context(), HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
		return HandleResult{}, errors.New("poison")
	}))
	require.ErrorContains(t, err, "DLQ unavailable")
	require.Zero(t, committer.count())
	require.Equal(t, 1, consumer.fetchCount())
}

func TestDLQRedeliveryUsesDeterministicIdentityInNonTransactionalMode(t *testing.T) {
	message := kafkaMessage(9)
	publisher := &fakePublisher{}
	newSource := func(commitErr error) (*KafkaSource, *fakeCommitter) {
		source, _, committer := testSource(t, func(config *Config) {
			config.PoisonPolicy = PoisonDLQ
			config.DLQTopic = "facts.dlq"
			config.DLQDeliveryMode = DLQAtLeastOnceNonTransactional
			config.MaxAttempts = 1
		}, message)
		committer.err = commitErr
		require.NoError(t, source.SetDLQPublisher(publisher))
		return source, committer
	}
	first, _ := newSource(errors.New("commit outage"))
	require.Error(t, first.Run(t.Context(), HandlerFunc(func(context.Context, Delivery) (HandleResult, error) { return HandleResult{}, errors.New("poison") })))
	second, committer := newSource(nil)
	require.NoError(t, second.Run(t.Context(), HandlerFunc(func(context.Context, Delivery) (HandleResult, error) { return HandleResult{}, errors.New("poison") })))
	require.Len(t, publisher.messages, 2, "non-transactional crash window permits duplicate publication")
	require.Equal(t, publisher.messages[0].Key, publisher.messages[1].Key)
	require.Equal(t, "kafka/cluster/facts/2/9/dlq", string(publisher.messages[0].Key))
	require.Equal(t, 1, committer.count())
}

func TestAttemptTrackerMemoryIsBoundedByUncommittedDeliveries(t *testing.T) {
	messages := make([]segmentio.Message, 1000)
	for index := range messages {
		messages[index] = kafkaMessage(int64(index))
	}
	source, _, _ := testSource(t, nil, messages...)
	tracker := newMemoryAttemptTracker()
	require.NoError(t, source.SetAttemptTracker(tracker))
	require.NoError(t, source.Run(t.Context(), HandlerFunc(func(context.Context, Delivery) (HandleResult, error) { return HandleResult{Completed: true}, nil })))
	tracker.mu.Lock()
	retained := len(tracker.attempts)
	tracker.mu.Unlock()
	require.Zero(t, retained)
}

func TestReadinessTracksRunAndFatalCommitFailure(t *testing.T) {
	source, _, committer := testSource(t, nil, kafkaMessage(1))
	require.Error(t, source.Ready())
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- source.Run(t.Context(), HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
			close(entered)
			<-release
			return HandleResult{Completed: true}, nil
		}))
	}()
	<-entered
	require.NoError(t, source.Ready())
	committer.err = errors.New("commit coordinator unavailable")
	close(release)
	require.Error(t, <-done)
	require.ErrorContains(t, source.Ready(), "commit coordinator unavailable")
}

func TestShutdownStillCommitsHandlerThatAlreadyCompleted(t *testing.T) {
	source, _, committer := testSource(t, nil, kafkaMessage(1))
	ctx, cancel := context.WithCancel(t.Context())
	err := source.Run(ctx, HandlerFunc(func(context.Context, Delivery) (HandleResult, error) {
		cancel()
		return HandleResult{Completed: true}, nil
	}))
	require.NoError(t, err)
	require.Equal(t, 1, committer.count())
}

func TestCancellationLeavesUnresolvedRecordUncommitted(t *testing.T) {
	source, _, committer := testSource(t, nil, kafkaMessage(1), kafkaMessage(2))
	ctx, cancel := context.WithCancel(t.Context())
	err := source.Run(ctx, HandlerFunc(func(handlerContext context.Context, delivery Delivery) (HandleResult, error) {
		if delivery.Message.Offset == 1 {
			return HandleResult{Completed: true}, nil
		}
		cancel()
		<-handlerContext.Done()
		return HandleResult{}, handlerContext.Err()
	}))
	require.NoError(t, err)
	require.Equal(t, 1, committer.count())
}

func TestGenerationCancellationReachesHandlerContext(t *testing.T) {
	runContext, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()
	generationContext, cancelGeneration := context.WithCancel(t.Context())
	merged, stop := mergeGenerationContext(runContext, generationContext)
	defer stop()
	cancelGeneration()
	select {
	case <-merged.Done():
	case <-time.After(time.Second):
		t.Fatal("generation cancellation did not reach handler context")
	}
}

func TestDeliveryIDIsStable(t *testing.T) {
	message := kafkaMessage(42)
	require.Equal(t, DeliveryID("cluster", message), DeliveryID("cluster", cloneMessage(message)))
	require.Equal(t, "kafka/cluster/facts/2/42", DeliveryID("cluster", message))
}
