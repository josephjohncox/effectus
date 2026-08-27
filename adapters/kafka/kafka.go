package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"

	"github.com/effectus/effectus-go/adapters"
)

// KafkaSource implements the FactSource interface for Kafka
type KafkaSource struct {
	config          *Config
	consumer        recordConsumer
	consumerFactory func() (recordConsumer, error)
	dlqPublisher    MessagePublisher
	poisonAck       PoisonAcknowledger
	attemptTracker  AttemptTracker
	factChan        chan *adapters.TypedFact
	converter       *MessageConverter
	metrics         adapters.SourceMetrics

	mu        sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
	runErr    error
	started   bool
	runActive bool
	unhealthy error
}

// Config holds Kafka source configuration
type Config struct {
	SourceID         string            `json:"source_id" yaml:"source_id"`
	ClusterNamespace string            `json:"cluster_namespace" yaml:"cluster_namespace"`
	Brokers          []string          `json:"brokers" yaml:"brokers"`
	Topic            string            `json:"topic" yaml:"topic"`
	ConsumerGroup    string            `json:"consumer_group" yaml:"consumer_group"`
	SchemaFormat     string            `json:"schema_format" yaml:"schema_format"`
	StartOffset      string            `json:"start_offset" yaml:"start_offset"`
	FactMappings     map[string]string `json:"fact_mappings" yaml:"fact_mappings"`
	Headers          map[string]string `json:"headers" yaml:"headers"`

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

// MessageConverter converts Kafka messages to TypedFacts
type MessageConverter struct {
	config       *Config
	factMappings map[string]string
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
		factChan:       make(chan *adapters.TypedFact),
		converter:      &MessageConverter{config: config, factMappings: config.FactMappings},
		metrics:        adapters.GetGlobalMetrics(),
	}
}

// SetPoisonAcknowledger configures the durable poison audit boundary.
// Call this before Run or Start.
func (source *KafkaSource) SetPoisonAcknowledger(acknowledger PoisonAcknowledger) error {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.started || source.runActive {
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
	if source.started || source.runActive {
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
	if source.started || source.runActive {
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
	resolved.FactMappings = cloneMappings(config.FactMappings)
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

func cloneMappings(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

// Start implements the legacy channel API. A channel send is only a local
// handoff, so this mode does not provide end-to-end acknowledgement.
func (k *KafkaSource) Start(ctx context.Context) error {
	k.mu.Lock()
	if k.started {
		k.mu.Unlock()
		return fmt.Errorf("source already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	k.cancel = cancel
	k.done = make(chan struct{})
	k.started = true
	k.runActive = true
	k.runErr = nil
	k.mu.Unlock()
	go func() {
		err := k.run(runCtx, HandlerFunc(func(handlerContext context.Context, delivery Delivery) (HandleResult, error) {
			fact, err := k.converter.ConvertMessage(delivery.Message)
			if err != nil {
				return HandleResult{}, err
			}
			select {
			case <-handlerContext.Done():
				return HandleResult{}, handlerContext.Err()
			case k.factChan <- fact:
				return HandleResult{Completed: true}, nil
			}
		}))
		k.mu.Lock()
		k.runErr = err
		k.started = false
		k.runActive = false
		close(k.done)
		k.mu.Unlock()
	}()
	log.Printf("Kafka source %s started for topic %s", k.config.SourceID, k.config.Topic)
	return nil
}

// Stop cancels admission and waits for the active handler to return.
func (k *KafkaSource) Stop(ctx context.Context) error {
	k.mu.Lock()
	cancel := k.cancel
	done := k.done
	wasStarted := k.started
	k.mu.Unlock()
	if !wasStarted && done == nil {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	k.mu.Lock()
	err := k.runErr
	k.done = nil
	k.cancel = nil
	k.mu.Unlock()
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	log.Printf("Kafka source %s stopped", k.config.SourceID)
	return err
}

// Subscribe returns the legacy local handoff channel.
func (k *KafkaSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.started {
		return nil, fmt.Errorf("source not started")
	}
	return k.factChan, nil
}

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
			k.metrics.RecordFactProcessed(k.config.SourceID, "kafka_record")
			return nil
		}
		lastError = err
		k.metrics.RecordError(k.config.SourceID, "handle_message", err)
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
	return committer.Commit(commitContext, message)
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
	k.metrics.RecordHealthCheck(k.config.SourceID, false)
}

// GetSourceSchema implements FactSource.GetSourceSchema
func (k *KafkaSource) GetSourceSchema() *adapters.Schema {
	return &adapters.Schema{
		Name:    fmt.Sprintf("kafka_%s", k.config.Topic),
		Version: "v1.0.0",
		Fields: map[string]interface{}{
			"topic":     "string",
			"partition": "int32",
			"offset":    "int64",
			"key":       "bytes",
			"value":     "bytes",
			"headers":   "map[string]string",
		},
	}
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

	k.metrics.RecordHealthCheck(k.config.SourceID, true)
	return nil
}

// GetMetadata implements FactSource.GetMetadata
func (k *KafkaSource) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      k.config.SourceID,
		SourceType:    "kafka",
		Version:       "v1.0.0",
		Capabilities:  []string{"streaming", "realtime", "backfill"},
		SchemaFormats: []string{"protobuf", "json", "avro"},
		Config: map[string]string{
			"topic":          k.config.Topic,
			"consumer_group": k.config.ConsumerGroup,
			"schema_format":  k.config.SchemaFormat,
		},
		Tags: []string{"messaging", "streaming"},
	}
}

// ConvertMessage converts a Kafka message to a TypedFact
func (c *MessageConverter) ConvertMessage(msg kafka.Message) (*adapters.TypedFact, error) {
	// Extract metadata from headers
	headers := make(map[string]string)
	var schemaID string
	var schemaVersion string
	var traceID string

	for _, header := range msg.Headers {
		key := string(header.Key)
		value := string(header.Value)
		headers[key] = value

		switch key {
		case "schema-id", "schema_id":
			schemaID = value
		case "schema-version", "schema_version":
			schemaVersion = value
		case "trace-id", "trace_id", "x-trace-id":
			traceID = value
		}
	}

	// Determine fact type from schema ID or topic
	var factType string
	if schemaID != "" {
		if mapped, exists := c.factMappings[schemaID]; exists {
			factType = mapped
		} else {
			return nil, fmt.Errorf("unknown schema ID: %s", schemaID)
		}
	} else {
		// Fallback to topic-based mapping
		if mapped, exists := c.factMappings[msg.Topic]; exists {
			factType = mapped
		} else {
			return nil, fmt.Errorf("no mapping for topic: %s", msg.Topic)
		}
	}

	// Convert message based on format
	var protoMsg proto.Message
	var err error

	switch c.config.SchemaFormat {
	case "json":
		protoMsg, err = c.convertJSONMessage(msg.Value, factType)
	case "protobuf":
		protoMsg, err = c.convertProtobufMessage(msg.Value, factType)
	case "avro":
		return nil, fmt.Errorf("avro format not yet supported")
	default:
		return nil, fmt.Errorf("unsupported schema format: %s", c.config.SchemaFormat)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to convert message: %w", err)
	}

	// Create TypedFact
	fact := &adapters.TypedFact{
		SchemaName:    factType,
		SchemaVersion: schemaVersion,
		Data:          protoMsg,
		RawData:       msg.Value,
		Timestamp:     msg.Time,
		SourceID:      c.config.SourceID,
		TraceID:       traceID,
		Metadata: map[string]string{
			"kafka.topic":     msg.Topic,
			"kafka.partition": fmt.Sprintf("%d", msg.Partition),
			"kafka.offset":    fmt.Sprintf("%d", msg.Offset),
			"kafka.key":       string(msg.Key),
		},
	}

	// Add custom headers to metadata
	for k, v := range headers {
		if !strings.HasPrefix(k, "kafka.") {
			fact.Metadata[k] = v
		}
	}

	return fact, nil
}

// convertJSONMessage converts JSON message to proto message
func (c *MessageConverter) convertJSONMessage(data []byte, factType string) (proto.Message, error) {
	// Parse JSON
	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Convert to proto based on fact type
	// This is a simplified conversion - in practice you'd use reflection
	// or code generation based on the proto schemas
	switch factType {
	case "acme.v1.facts.UserProfile":
		return c.convertToUserProfile(jsonData)
	case "acme.v1.facts.SystemEvent":
		return c.convertToSystemEvent(jsonData)
	default:
		// Generic proto message - would need proper schema-driven conversion
		return nil, fmt.Errorf("unsupported fact type for JSON conversion: %s", factType)
	}
}

// convertProtobufMessage converts protobuf message
func (c *MessageConverter) convertProtobufMessage(data []byte, factType string) (proto.Message, error) {
	// This would use proto.Unmarshal with the appropriate message type
	// For now, return an error since we don't have the generated types
	return nil, fmt.Errorf("protobuf conversion not implemented for fact type: %s", factType)
}

// Helper conversion functions (simplified examples)
func (c *MessageConverter) convertToUserProfile(data map[string]interface{}) (proto.Message, error) {
	// This would create an actual UserProfile proto message
	// For now, return nil as we don't have the generated types
	return nil, fmt.Errorf("UserProfile conversion not implemented")
}

func (c *MessageConverter) convertToSystemEvent(data map[string]interface{}) (proto.Message, error) {
	// This would create an actual SystemEvent proto message
	// For now, return nil as we don't have the generated types
	return nil, fmt.Errorf("SystemEvent conversion not implemented")
}

// Factory for Kafka sources
type Factory struct{}

func (f *Factory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	kafkaConfig := &Config{
		SourceID: config.SourceID,
	}

	// Extract Kafka-specific configuration
	switch brokers := config.Config["brokers"].(type) {
	case []interface{}:
		for _, broker := range brokers {
			if value, ok := broker.(string); ok {
				kafkaConfig.Brokers = append(kafkaConfig.Brokers, value)
			}
		}
	case []string:
		kafkaConfig.Brokers = append(kafkaConfig.Brokers, brokers...)
	}

	if topic, ok := config.Config["topic"].(string); ok {
		kafkaConfig.Topic = topic
	}

	if group, ok := config.Config["consumer_group"].(string); ok {
		kafkaConfig.ConsumerGroup = group
	}

	if format, ok := config.Config["schema_format"].(string); ok {
		kafkaConfig.SchemaFormat = format
	}
	if namespace, ok := config.Config["cluster_namespace"].(string); ok {
		kafkaConfig.ClusterNamespace = namespace
	}
	if startOffset, ok := config.Config["start_offset"].(string); ok {
		kafkaConfig.StartOffset = startOffset
	}
	if contract, ok := config.Config["ack_contract"].(string); ok {
		kafkaConfig.AckContract = AckContract(contract)
	}
	if attempts, ok := config.Config["max_attempts"].(int); ok {
		kafkaConfig.MaxAttempts = attempts
	}
	if policy, ok := config.Config["poison_policy"].(string); ok {
		kafkaConfig.PoisonPolicy = PoisonPolicy(policy)
	}
	if topic, ok := config.Config["dlq_topic"].(string); ok {
		kafkaConfig.DLQTopic = topic
	}
	if mode, ok := config.Config["dlq_delivery_mode"].(string); ok {
		kafkaConfig.DLQDeliveryMode = DLQDeliveryMode(mode)
	}
	if value, ok := config.Config["initial_backoff"].(string); ok {
		if duration, err := time.ParseDuration(value); err == nil {
			kafkaConfig.InitialBackoff = duration
		}
	}
	if value, ok := config.Config["max_backoff"].(string); ok {
		if duration, err := time.ParseDuration(value); err == nil {
			kafkaConfig.MaxBackoff = duration
		}
	}

	// Convert mappings
	kafkaConfig.FactMappings = make(map[string]string)
	for _, mapping := range config.Mappings {
		kafkaConfig.FactMappings[mapping.SourceKey] = mapping.EffectusType
	}

	return NewKafkaSource(kafkaConfig)
}

func (f *Factory) ValidateConfig(config adapters.SourceConfig) error {
	// Validate required fields
	if _, ok := config.Config["brokers"]; !ok {
		return fmt.Errorf("brokers is required")
	}
	if _, ok := config.Config["topic"]; !ok {
		return fmt.Errorf("topic is required")
	}
	if _, ok := config.Config["consumer_group"]; !ok {
		return fmt.Errorf("consumer_group is required")
	}
	return nil
}

func (f *Factory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"brokers": {
				Type:        "array",
				Description: "List of Kafka broker addresses",
				Examples:    []string{"[\"localhost:9092\"]", "[\"kafka1:9092\", \"kafka2:9092\"]"},
			},
			"topic": {
				Type:        "string",
				Description: "Kafka topic to consume from",
				Examples:    []string{"user.events", "system.logs"},
			},
			"consumer_group": {
				Type:        "string",
				Description: "Kafka consumer group ID",
				Examples:    []string{"effectus_consumers", "my_app_group"},
			},
			"schema_format": {
				Type:        "string",
				Description: "Message format",
				Default:     "json",
				Examples:    []string{"json", "protobuf", "avro"},
			},
			"start_offset": {
				Type:        "string",
				Description: "Where to start consuming",
				Default:     "latest",
				Examples:    []string{"earliest", "latest"},
			},
			"cluster_namespace": {
				Type:        "string",
				Description: "Stable cluster name used in delivery identities",
			},
			"ack_contract": {
				Type:        "string",
				Description: "Offset acknowledgement boundary",
				Default:     string(AckAfterCompletedProcessing),
				Examples:    []string{string(AckAfterCompletedProcessing), string(AckAfterDurableAcceptance)},
			},
			"poison_policy": {
				Type:        "string",
				Description: "Action after attempts are exhausted",
				Default:     string(PoisonHalt),
				Examples:    []string{string(PoisonHalt), string(PoisonSkip), string(PoisonDLQ)},
			},
			"dlq_topic": {Type: "string", Description: "DLQ topic used by the dlq poison policy"},
			"dlq_delivery_mode": {
				Type:        "string",
				Description: "Explicit non-atomic DLQ delivery contract; duplicates can occur between DLQ publish and source-offset commit",
				Examples:    []string{string(DLQAtLeastOnceNonTransactional)},
			},
		},
		Required: []string{"brokers", "topic", "consumer_group"},
	}
}

// Register the Kafka source factory
func init() {
	adapters.RegisterSourceType("kafka", &Factory{})
}
