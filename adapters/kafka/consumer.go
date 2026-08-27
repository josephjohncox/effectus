package kafka

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"

	segmentio "github.com/segmentio/kafka-go"
)

type recordCommitter interface {
	Commit(context.Context, segmentio.Message) error
}

type recordConsumer interface {
	Run(context.Context, func(context.Context, segmentio.Message, recordCommitter) error) error
	Close() error
}

type groupClient interface {
	Next(context.Context) (*segmentio.Generation, error)
	Close() error
}

type partitionReader interface {
	FetchMessage(context.Context) (segmentio.Message, error)
	SetOffset(int64) error
	Close() error
}

type partitionReaderFactory func(topic string, partition int) partitionReader

type consumerGroupRunner struct {
	group         groupClient
	readerFactory partitionReaderFactory
	ready         atomic.Bool
}

func newConsumerGroupRunner(config *Config) (*consumerGroupRunner, error) {
	startOffset := segmentio.LastOffset
	if config.StartOffset == "earliest" {
		startOffset = segmentio.FirstOffset
	}
	group, err := segmentio.NewConsumerGroup(segmentio.ConsumerGroupConfig{
		ID:                     config.ConsumerGroup,
		Brokers:                append([]string(nil), config.Brokers...),
		Topics:                 []string{config.Topic},
		GroupBalancers:         []segmentio.GroupBalancer{segmentio.RangeGroupBalancer{}},
		StartOffset:            startOffset,
		WatchPartitionChanges:  true,
		PartitionWatchInterval: config.PartitionWatchInterval,
		HeartbeatInterval:      config.HeartbeatInterval,
		SessionTimeout:         config.SessionTimeout,
		RebalanceTimeout:       config.RebalanceTimeout,
		JoinGroupBackoff:       config.JoinGroupBackoff,
	})
	if err != nil {
		return nil, fmt.Errorf("create Kafka consumer group: %w", err)
	}
	factory := func(topic string, partition int) partitionReader {
		return segmentio.NewReader(segmentio.ReaderConfig{
			Brokers:       append([]string(nil), config.Brokers...),
			Topic:         topic,
			Partition:     partition,
			MinBytes:      config.MinBytes,
			MaxBytes:      config.MaxBytes,
			QueueCapacity: 1,
		})
	}
	return &consumerGroupRunner{group: group, readerFactory: factory}, nil
}

func (runner *consumerGroupRunner) Run(ctx context.Context, process func(context.Context, segmentio.Message, recordCommitter) error) (runErr error) {
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	defer func() { runErr = errors.Join(runErr, runner.group.Close()) }()

	// Acquiring this token before FetchMessage limits application-level
	// admission to one fetched record across all assigned partitions.
	admission := make(chan struct{}, 1)
	admission <- struct{}{}

	for {
		generation, err := runner.group.Next(runCtx)
		if err != nil {
			runner.ready.Store(false)
			// Wait for a handler that already returned success to finish its
			// synchronous commit before Close ends the generation.
			<-admission
			admission <- struct{}{}
			if ctx.Err() != nil || errors.Is(err, segmentio.ErrGroupClosed) {
				return nil
			}
			if cause := context.Cause(runCtx); cause != nil && !errors.Is(cause, context.Canceled) {
				return cause
			}
			return err
		}
		runner.ready.Store(true)
		assignments := orderedAssignments(generation.Assignments)
		if len(assignments) == 0 {
			generation.Start(func(generationContext context.Context) {
				defer runner.ready.Store(false)
				<-generationContext.Done()
			})
			continue
		}
		for _, assignment := range assignments {
			assignment := assignment
			generation.Start(func(generationContext context.Context) {
				defer runner.ready.Store(false)
				workerCtx, stop := mergeGenerationContext(runCtx, generationContext)
				defer stop()
				reader := runner.readerFactory(assignment.topic, assignment.partition.ID)
				if err := reader.SetOffset(assignment.partition.Offset); err != nil {
					cancel(fmt.Errorf("set Kafka offset for %s/%d: %w", assignment.topic, assignment.partition.ID, err))
					_ = reader.Close()
					return
				}
				defer reader.Close()
				committer := generationCommitter{generation: generation}
				for {
					select {
					case <-workerCtx.Done():
						return
					case <-admission:
					}
					message, err := reader.FetchMessage(workerCtx)
					if err != nil {
						admission <- struct{}{}
						if workerCtx.Err() != nil || errors.Is(err, segmentio.ErrGenerationEnded) {
							return
						}
						cancel(fmt.Errorf("fetch Kafka message: %w", err))
						return
					}
					if err := process(workerCtx, message, committer); err != nil {
						if workerCtx.Err() == nil {
							cancel(err)
						}
						admission <- struct{}{}
						return
					}
					admission <- struct{}{}
				}
			})
		}
	}
}

func (runner *consumerGroupRunner) Ready() bool { return runner.ready.Load() }
func (runner *consumerGroupRunner) Close() error {
	runner.ready.Store(false)
	return runner.group.Close()
}

type generationCommitter struct {
	generation *segmentio.Generation
}

func (committer generationCommitter) Commit(ctx context.Context, message segmentio.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := committer.generation.CommitOffsets(map[string]map[int]int64{
		message.Topic: {message.Partition: message.Offset + 1},
	})
	if err != nil {
		return fmt.Errorf("commit Kafka offset %s/%d/%d: %w", message.Topic, message.Partition, message.Offset, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

type topicAssignment struct {
	topic     string
	partition segmentio.PartitionAssignment
}

func orderedAssignments(assignments map[string][]segmentio.PartitionAssignment) []topicAssignment {
	result := make([]topicAssignment, 0)
	for topic, partitions := range assignments {
		for _, partition := range partitions {
			result = append(result, topicAssignment{topic: topic, partition: partition})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].topic != result[j].topic {
			return result[i].topic < result[j].topic
		}
		return result[i].partition.ID < result[j].partition.ID
	})
	return result
}

func mergeGenerationContext(runContext, generationContext context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(runContext)
	stop := context.AfterFunc(generationContext, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}
