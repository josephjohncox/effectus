package kafka

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
)

// KafkaSource admits Kafka records through the daemon's durable execution boundary.
type KafkaSource struct {
	config          *Config
	consumer        recordConsumer
	consumerFactory func() (recordConsumer, error)
	dlqPublisher    MessagePublisher
	poisonAck       PoisonAcknowledger
	attemptTracker  AttemptTracker
	mu              sync.Mutex
	runActive       bool
	unhealthy       error

	lastFetchedOffset   atomic.Int64
	lastCommittedOffset atomic.Int64
	highWatermark       atomic.Int64
	commitHealthy       atomic.Bool
}

// Config holds the daemon Kafka-admission configuration.
type Config struct {
	SourceID         string   `json:"source_id" yaml:"source_id"`
	ClusterNamespace string   `json:"cluster_namespace" yaml:"cluster_namespace"`
	Brokers          []string `json:"brokers" yaml:"brokers"`
	Topic            string   `json:"topic" yaml:"topic"`
	ConsumerGroup    string   `json:"consumer_group" yaml:"consumer_group"`
	StartOffset      string   `json:"start_offset" yaml:"start_offset"`

	AckContract            AckContract     `json:"ack_contract" yaml:"ack_contract"`
	MaxAttempts            int             `json:"max_attempts" yaml:"max_attempts"`
	InitialBackoff         time.Duration   `json:"initial_backoff" yaml:"initial_backoff"`
	MaxBackoff             time.Duration   `json:"max_backoff" yaml:"max_backoff"`
	CommitTimeout          time.Duration   `json:"commit_timeout" yaml:"commit_timeout"`
	PoisonPolicy           PoisonPolicy    `json:"poison_policy" yaml:"poison_policy"`
	DLQTopic               string          `json:"dlq_topic" yaml:"dlq_topic"`
	DLQDeliveryMode        DLQDeliveryMode `json:"dlq_delivery_mode" yaml:"dlq_delivery_mode"`
	MinBytes               int             `json:"min_bytes" yaml:"min_bytes"`
	MaxBytes               int             `json:"max_bytes" yaml:"max_bytes"`
	HeartbeatInterval      time.Duration   `json:"heartbeat_interval" yaml:"heartbeat_interval"`
	SessionTimeout         time.Duration   `json:"session_timeout" yaml:"session_timeout"`
	RebalanceTimeout       time.Duration   `json:"rebalance_timeout" yaml:"rebalance_timeout"`
	JoinGroupBackoff       time.Duration   `json:"join_group_backoff" yaml:"join_group_backoff"`
	PartitionWatchInterval time.Duration   `json:"partition_watch_interval" yaml:"partition_watch_interval"`
}

// AckContract selects the condition required before an offset commit.
type AckContract string

const (
	AckAfterDurableAcceptance   AckContract = "durable_acceptance"
	AckAfterCompletedProcessing AckContract = "completed_processing"
)

// PoisonPolicy controls records that exhaust the configured attempts.
type PoisonPolicy string

const (
	PoisonHalt PoisonPolicy = "halt"
	PoisonSkip PoisonPolicy = "skip"
	PoisonDLQ  PoisonPolicy = "dlq"
)

// DLQDeliveryMode describes the crash window between DLQ publication and
// source-offset commit. kafka-go does not combine these operations here.
type DLQDeliveryMode string

const DLQAtLeastOnceNonTransactional DLQDeliveryMode = "at_least_once_non_transactional"

// Delivery is one immutable Kafka record with a stable admission identity.
type Delivery struct {
	ID      string
	Message kafka.Message
	Attempt int
}

// HandleResult states which acknowledgement boundary the handler reached.
type HandleResult struct {
	DurablyAccepted bool
	Completed       bool
}

// Handler blocks until it reaches an acknowledgement boundary or returns an error.
type Handler interface {
	Handle(context.Context, Delivery) (HandleResult, error)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(context.Context, Delivery) (HandleResult, error)

func (function HandlerFunc) Handle(ctx context.Context, delivery Delivery) (HandleResult, error) {
	return function(ctx, delivery)
}

// PoisonDisposition is the durable audit input for skip and DLQ policies.
type PoisonDisposition struct {
	DeliveryID string
	Policy     PoisonPolicy
	Attempts   int
	Error      string
	Message    kafka.Message
}

// PoisonAcknowledger durably records an operator-selected poison disposition.
type PoisonAcknowledger interface {
	AcknowledgePoison(context.Context, PoisonDisposition) error
}

type poisonAcknowledgementLookup interface {
	PoisonAcknowledged(context.Context, string) (bool, error)
}

// AttemptTracker durably counts handler failures by stable delivery identity.
// A crash, rebalance, or commit outage after a successful handler must not
// consume a poison attempt.
type AttemptTracker interface {
	Attempts(context.Context, string) (int, error)
	RecordFailure(context.Context, string) (int, error)
	ClearAttempts(context.Context, string) error
}

// MessagePublisher publishes a DLQ record and returns only after broker acknowledgement.
type MessagePublisher interface {
	WriteMessages(context.Context, ...kafka.Message) error
}

var ErrPoisonMessage = errors.New("Kafka poison message halted consumption")

type memoryAttemptTracker struct {
	mu       sync.Mutex
	attempts map[string]int
}

func newMemoryAttemptTracker() *memoryAttemptTracker {
	return &memoryAttemptTracker{attempts: make(map[string]int)}
}

// NewMemoryAttemptTracker returns process-local tracking for tests only.
// Production consumers must use durable tracking.
func NewMemoryAttemptTracker() AttemptTracker { return newMemoryAttemptTracker() }

func (tracker *memoryAttemptTracker) Attempts(_ context.Context, deliveryID string) (int, error) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.attempts[deliveryID], nil
}

func (tracker *memoryAttemptTracker) RecordFailure(_ context.Context, deliveryID string) (int, error) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.attempts[deliveryID]++
	return tracker.attempts[deliveryID], nil
}

func (tracker *memoryAttemptTracker) ClearAttempts(_ context.Context, deliveryID string) error {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.attempts, deliveryID)
	return nil
}

// NewKafkaSource creates a new Kafka fact source.
func NewKafkaSource(config *Config) (*KafkaSource, error) {
	resolved, err := normalizeConfig(config)
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	var publisher MessagePublisher
	if resolved.PoisonPolicy == PoisonDLQ {
		publisher = &kafka.Writer{
			Addr: kafka.TCP(resolved.Brokers...), Topic: resolved.DLQTopic,
			Balancer: &kafka.LeastBytes{}, RequiredAcks: kafka.RequireAll,
		}
	}
	source := newKafkaSource(resolved, nil, publisher, nil)
	// Production callers must install durable tracking before Run.
	source.attemptTracker = nil
	source.consumerFactory = func() (recordConsumer, error) { return newConsumerGroupRunner(resolved) }
	return source, nil
}

func newKafkaSource(config *Config, consumer recordConsumer, publisher MessagePublisher, poisonAck PoisonAcknowledger) *KafkaSource {
	return &KafkaSource{
		config: config, consumer: consumer, dlqPublisher: publisher, poisonAck: poisonAck,
		attemptTracker: newMemoryAttemptTracker(),
	}
}

// SetPoisonAcknowledger configures the durable poison audit boundary.
// Call this before Run or Start.
func (source *KafkaSource) SetPoisonAcknowledger(acknowledger PoisonAcknowledger) error {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.runActive {
		return fmt.Errorf("cannot change poison acknowledger while Kafka source is running")
	}
	source.poisonAck = acknowledger
	return nil
}

// SetAttemptTracker configures durable attempt accounting across rebalances.
// Call this before Run or Start.
func (source *KafkaSource) SetAttemptTracker(tracker AttemptTracker) error {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.runActive {
		return fmt.Errorf("cannot change attempt tracker while Kafka source is running")
	}
	if tracker == nil {
		return fmt.Errorf("Kafka attempt tracker is required")
	}
	source.attemptTracker = tracker
	return nil
}

// SetDLQPublisher replaces the default Kafka writer for testing or custom publication.
func (source *KafkaSource) SetDLQPublisher(publisher MessagePublisher) error {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.runActive {
		return fmt.Errorf("cannot change DLQ publisher while Kafka source is running")
	}
	source.dlqPublisher = publisher
	return nil
}

func normalizeConfig(config *Config) (*Config, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	resolved := *config
	resolved.Brokers = append([]string(nil), config.Brokers...)
	if len(resolved.Brokers) == 0 {
		return nil, fmt.Errorf("brokers list is empty")
	}
	for _, broker := range resolved.Brokers {
		if strings.TrimSpace(broker) == "" {
			return nil, fmt.Errorf("broker address is empty")
		}
	}
	if strings.TrimSpace(resolved.Topic) == "" {
		return nil, fmt.Errorf("topic is required")
	}
	if strings.TrimSpace(resolved.ConsumerGroup) == "" {
		return nil, fmt.Errorf("consumer group is required")
	}
	if strings.TrimSpace(resolved.SourceID) == "" {
		return nil, fmt.Errorf("source ID is required")
	}
	if resolved.ClusterNamespace == "" {
		resolved.ClusterNamespace = resolved.SourceID
	}
	if strings.Contains(resolved.ClusterNamespace, "/") {
		return nil, fmt.Errorf("cluster namespace must not contain '/'")
	}
	if resolved.StartOffset == "" {
		resolved.StartOffset = "latest"
	}
	if resolved.StartOffset != "earliest" && resolved.StartOffset != "latest" {
		return nil, fmt.Errorf("start offset must be earliest or latest")
	}
	if resolved.AckContract == "" {
		resolved.AckContract = AckAfterCompletedProcessing
	}
	if resolved.AckContract != AckAfterDurableAcceptance && resolved.AckContract != AckAfterCompletedProcessing {
		return nil, fmt.Errorf("unsupported acknowledgement contract %q", resolved.AckContract)
	}
	if resolved.MaxAttempts == 0 {
		resolved.MaxAttempts = 3
	}
	if resolved.MaxAttempts < 1 {
		return nil, fmt.Errorf("max attempts must be positive")
	}
	if resolved.InitialBackoff == 0 {
		resolved.InitialBackoff = time.Second
	}
	if resolved.MaxBackoff == 0 {
		resolved.MaxBackoff = 30 * time.Second
	}
	if resolved.CommitTimeout == 0 {
		resolved.CommitTimeout = 10 * time.Second
	}
	if resolved.CommitTimeout < 0 {
		return nil, fmt.Errorf("commit timeout must not be negative")
	}
	if resolved.InitialBackoff < 0 || resolved.MaxBackoff < resolved.InitialBackoff {
		return nil, fmt.Errorf("invalid retry backoff")
	}
	if resolved.PoisonPolicy == "" {
		resolved.PoisonPolicy = PoisonHalt
	}
	if resolved.PoisonPolicy != PoisonHalt && resolved.PoisonPolicy != PoisonSkip && resolved.PoisonPolicy != PoisonDLQ {
		return nil, fmt.Errorf("unsupported poison policy %q", resolved.PoisonPolicy)
	}
	if resolved.PoisonPolicy == PoisonDLQ {
		if strings.TrimSpace(resolved.DLQTopic) == "" {
			return nil, fmt.Errorf("DLQ topic is required for the dlq poison policy")
		}
		if resolved.DLQDeliveryMode != DLQAtLeastOnceNonTransactional {
			return nil, fmt.Errorf("DLQ delivery mode must explicitly be %q; atomic DLQ publication and source-offset commit are not enabled", DLQAtLeastOnceNonTransactional)
		}
	}
	if resolved.MinBytes == 0 {
		resolved.MinBytes = 1
	}
	if resolved.MaxBytes == 0 {
		resolved.MaxBytes = 10 << 20
	}
	if resolved.MinBytes < 1 || resolved.MaxBytes < resolved.MinBytes {
		return nil, fmt.Errorf("invalid Kafka fetch byte limits")
	}
	if resolved.HeartbeatInterval == 0 {
		resolved.HeartbeatInterval = 3 * time.Second
	}
	if resolved.SessionTimeout == 0 {
		resolved.SessionTimeout = 30 * time.Second
	}
	if resolved.RebalanceTimeout == 0 {
		resolved.RebalanceTimeout = 30 * time.Second
	}
	if resolved.JoinGroupBackoff == 0 {
		resolved.JoinGroupBackoff = 5 * time.Second
	}
	if resolved.PartitionWatchInterval == 0 {
		resolved.PartitionWatchInterval = 5 * time.Second
	}
	return &resolved, nil
}

// ValidateConfig checks source configuration without opening consumer resources.
func ValidateConfig(config *Config) error {
	_, err := normalizeConfig(config)
	return err
}

func validateConfig(config *Config) error { return ValidateConfig(config) }

// Run consumes one application-level record at a time. It fetches no later
// record until the current record reaches the configured acknowledgement boundary.
func (k *KafkaSource) Run(ctx context.Context, handler Handler) error {
	if handler == nil {
		return fmt.Errorf("Kafka handler is required")
	}
	k.mu.Lock()
	if k.runActive {
		k.mu.Unlock()
		return fmt.Errorf("Kafka source is already running")
	}
	k.runActive = true
	k.mu.Unlock()
	defer func() {
		k.mu.Lock()
		k.runActive = false
		k.mu.Unlock()
	}()
	return k.run(ctx, handler)
}

func (k *KafkaSource) run(ctx context.Context, handler Handler) (runErr error) {
	k.mu.Lock()
	consumer, factory := k.consumer, k.consumerFactory
	k.mu.Unlock()
	createdConsumer := false
	if consumer == nil {
		if factory == nil {
			return fmt.Errorf("Kafka consumer is not configured")
		}
		var err error
		consumer, err = factory()
		if err != nil {
			return err
		}
		k.mu.Lock()
		k.consumer = consumer
		k.mu.Unlock()
		createdConsumer = true
	}
	if createdConsumer {
		defer func() { k.mu.Lock(); k.consumer = nil; k.mu.Unlock() }()
	}
	if closer, ok := k.dlqPublisher.(interface{ Close() error }); ok {
		defer func() { runErr = errors.Join(runErr, closer.Close()) }()
	}
	if k.attemptTracker == nil {
		return fmt.Errorf("Kafka durable attempt tracker is required")
	}
	if k.config.PoisonPolicy == PoisonSkip && k.poisonAck == nil {
		return fmt.Errorf("skip poison policy requires a durable poison acknowledger")
	}
	if k.config.PoisonPolicy == PoisonDLQ && k.dlqPublisher == nil {
		return fmt.Errorf("dlq poison policy requires an acknowledged publisher")
	}
	return consumer.Run(ctx, func(recordContext context.Context, message kafka.Message, committer recordCommitter) error {
		return k.processMessage(recordContext, message, committer, handler)
	})
}

func (k *KafkaSource) processMessage(ctx context.Context, message kafka.Message, committer recordCommitter, handler Handler) error {
	k.lastFetchedOffset.Store(message.Offset)
	k.highWatermark.Store(message.HighWaterMark)
	deliveryID := DeliveryID(k.config.ClusterNamespace, message)
	failures, err := k.attemptTracker.Attempts(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("read Kafka delivery attempts: %w", err)
	}
	var lastError error
	for {
		attempt := failures + 1
		result, err := handler.Handle(ctx, Delivery{ID: deliveryID, Message: cloneMessage(message), Attempt: attempt})
		if err == nil {
			err = k.validateHandleResult(result)
		}
		if err == nil {
			if err := k.commitMessage(ctx, committer, message); err != nil {
				k.markUnhealthy(err)
				return err
			}
			if err := k.attemptTracker.ClearAttempts(context.WithoutCancel(ctx), deliveryID); err != nil {
				return fmt.Errorf("clear committed Kafka delivery attempts: %w", err)
			}
			return nil
		}
		lastError = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		failures, err = k.attemptTracker.RecordFailure(ctx, deliveryID)
		if err != nil {
			return fmt.Errorf("persist Kafka delivery failure: %w", err)
		}
		if failures >= k.config.MaxAttempts {
			return k.handlePoison(ctx, message, deliveryID, failures, lastError, committer)
		}
		if err := waitContext(ctx, retryDelay(k.config.InitialBackoff, k.config.MaxBackoff, failures)); err != nil {
			return err
		}
	}
}

func (k *KafkaSource) validateHandleResult(result HandleResult) error {
	switch k.config.AckContract {
	case AckAfterDurableAcceptance:
		if !result.DurablyAccepted {
			return fmt.Errorf("handler returned before durable acceptance")
		}
	case AckAfterCompletedProcessing:
		if !result.Completed {
			return fmt.Errorf("handler returned before completed processing")
		}
	default:
		return fmt.Errorf("unsupported acknowledgement contract %q", k.config.AckContract)
	}
	return nil
}

func (k *KafkaSource) handlePoison(ctx context.Context, message kafka.Message, deliveryID string, attempts int, cause error, committer recordCommitter) error {
	disposition := PoisonDisposition{
		DeliveryID: deliveryID, Policy: k.config.PoisonPolicy,
		Attempts: attempts, Error: cause.Error(), Message: cloneMessage(message),
	}
	if lookup, ok := k.poisonAck.(poisonAcknowledgementLookup); ok {
		acknowledged, err := lookup.PoisonAcknowledged(ctx, deliveryID)
		if err != nil {
			return fmt.Errorf("read poison acknowledgement: %w", err)
		}
		if acknowledged {
			if err := k.commitMessage(ctx, committer, message); err != nil {
				return err
			}
			return k.attemptTracker.ClearAttempts(context.WithoutCancel(ctx), deliveryID)
		}
	}
	switch k.config.PoisonPolicy {
	case PoisonHalt:
		err := fmt.Errorf("%w: %s after %d attempts: %v", ErrPoisonMessage, deliveryID, k.config.MaxAttempts, cause)
		k.markUnhealthy(err)
		return err
	case PoisonSkip:
		if err := k.poisonAck.AcknowledgePoison(ctx, disposition); err != nil {
			return fmt.Errorf("acknowledge skipped poison record: %w", err)
		}
	case PoisonDLQ:
		dlqIdentity := deliveryID + "/dlq"
		dlqMessage := kafka.Message{
			Topic: k.config.DLQTopic, Key: []byte(dlqIdentity), Value: append([]byte(nil), message.Value...),
			Time: message.Time,
			Headers: append(cloneHeaders(message.Headers),
				kafka.Header{Key: "effectus-delivery-id", Value: []byte(deliveryID)},
				kafka.Header{Key: "effectus-dlq-id", Value: []byte(dlqIdentity)},
				kafka.Header{Key: "effectus-dlq-mode", Value: []byte(k.config.DLQDeliveryMode)},
				kafka.Header{Key: "effectus-error", Value: []byte(cause.Error())},
			),
		}
		if err := k.dlqPublisher.WriteMessages(ctx, dlqMessage); err != nil {
			return fmt.Errorf("publish poison record to DLQ: %w", err)
		}
		if k.poisonAck != nil {
			if err := k.poisonAck.AcknowledgePoison(ctx, disposition); err != nil {
				return fmt.Errorf("acknowledge DLQ poison record: %w", err)
			}
		}
	default:
		return fmt.Errorf("unsupported poison policy %q", k.config.PoisonPolicy)
	}
	if err := k.commitMessage(ctx, committer, message); err != nil {
		k.markUnhealthy(err)
		return err
	}
	if err := k.attemptTracker.ClearAttempts(context.WithoutCancel(ctx), deliveryID); err != nil {
		return fmt.Errorf("clear committed poison delivery attempts: %w", err)
	}
	return nil
}

func (k *KafkaSource) commitMessage(handlerContext context.Context, committer recordCommitter, message kafka.Message) error {
	commitContext := context.WithoutCancel(handlerContext)
	if k.config.CommitTimeout > 0 {
		var cancel context.CancelFunc
		commitContext, cancel = context.WithTimeout(commitContext, k.config.CommitTimeout)
		defer cancel()
	}
	if err := committer.Commit(commitContext, message); err != nil {
		k.commitHealthy.Store(false)
		return err
	}
	k.lastCommittedOffset.Store(message.Offset + 1)
	k.commitHealthy.Store(true)
	return nil
}

// ConsumerStatus is a nonblocking snapshot of the active Kafka boundary.
type ConsumerStatus struct {
	LastFetchedOffset   int64
	LastCommittedOffset int64
	HighWatermark       int64
	Lag                 int64
	CommitHealthy       bool
}

func (k *KafkaSource) ConsumerStatus() ConsumerStatus {
	status := ConsumerStatus{
		LastFetchedOffset: k.lastFetchedOffset.Load(), LastCommittedOffset: k.lastCommittedOffset.Load(),
		HighWatermark: k.highWatermark.Load(), CommitHealthy: k.commitHealthy.Load(),
	}
	status.Lag = status.HighWatermark - status.LastCommittedOffset
	if status.Lag < 0 {
		status.Lag = 0
	}
	return status
}

// DeliveryID is stable across consumer restarts and group rebalances.
func DeliveryID(clusterNamespace string, message kafka.Message) string {
	return fmt.Sprintf("kafka/%s/%s/%d/%d", clusterNamespace, message.Topic, message.Partition, message.Offset)
}

func retryDelay(initial, maximum time.Duration, attempt int) time.Duration {
	delay := initial
	for count := 1; count < attempt && delay < maximum; count++ {
		if delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func cloneMessage(message kafka.Message) kafka.Message {
	copy := message
	copy.Key = append([]byte(nil), message.Key...)
	copy.Value = append([]byte(nil), message.Value...)
	copy.Headers = cloneHeaders(message.Headers)
	return copy
}

func cloneHeaders(headers []kafka.Header) []kafka.Header {
	result := make([]kafka.Header, len(headers))
	for index, header := range headers {
		result[index] = kafka.Header{Key: header.Key, Value: append([]byte(nil), header.Value...)}
	}
	return result
}

func (k *KafkaSource) markUnhealthy(err error) {
	k.mu.Lock()
	k.unhealthy = err
	k.mu.Unlock()
}

// Ready reports whether the consumer loop is active and has not crossed a
// fatal commit or poison boundary. It does not perform a blocking broker dial.
func (k *KafkaSource) Ready() error {
	k.mu.Lock()
	active, unhealthy, consumer := k.runActive, k.unhealthy, k.consumer
	k.mu.Unlock()
	if unhealthy != nil {
		return unhealthy
	}
	if !active {
		return fmt.Errorf("Kafka consumer is not running")
	}
	if consumer == nil {
		return fmt.Errorf("Kafka consumer is initializing")
	}
	if readiness, ok := consumer.(interface{ Ready() bool }); ok && !readiness.Ready() {
		return fmt.Errorf("Kafka consumer has not joined an active generation")
	}
	return nil
}

// HealthCheck implements FactSource.HealthCheck
func (k *KafkaSource) HealthCheck() error {
	k.mu.Lock()
	unhealthy := k.unhealthy
	k.mu.Unlock()
	if unhealthy != nil {
		return unhealthy
	}
	// Try to get metadata from Kafka.
	healthContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := kafka.DialContext(healthContext, "tcp", k.config.Brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect to kafka: %w", err)
	}
	defer conn.Close()

	_, err = conn.ReadPartitions()
	if err != nil {
		return fmt.Errorf("failed to read partitions: %w", err)
	}

	return nil
}
