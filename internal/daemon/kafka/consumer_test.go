package kafka

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	segmentio "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

type scriptedGroup struct {
	next   func(context.Context, int32) (*segmentio.Generation, error)
	calls  atomic.Int32
	closed atomic.Bool
}

type offsetCommitterFunc func(map[string]map[int]int64) error

func (commit offsetCommitterFunc) CommitOffsets(offsets map[string]map[int]int64) error {
	return commit(offsets)
}

func TestConsumerGroupCommitTimeoutUsesConfiguredCoordinatorTimeout(t *testing.T) {
	for _, test := range []struct {
		name          string
		commitTimeout time.Duration
		want          time.Duration
	}{
		{name: "default", want: 10 * time.Second},
		{name: "configured", commitTimeout: 175 * time.Millisecond, want: 175 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := normalizeConfig(&Config{
				SourceID: "source", Brokers: []string{"broker:9092"},
				Topic: "facts", ConsumerGroup: "effectus", CommitTimeout: test.commitTimeout,
			})
			require.NoError(t, err)
			require.Equal(t, test.want, consumerGroupConfig(config).Timeout)
		})
	}
}

func TestGenerationCommitterPreservesSuccessfulCommitAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	committer := generationCommitter{generation: offsetCommitterFunc(func(offsets map[string]map[int]int64) error {
		require.Equal(t, map[string]map[int]int64{"facts": {2: 43}}, offsets)
		cancel()
		return nil
	})}

	require.NoError(t, committer.Commit(ctx, kafkaMessage(42)))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func (group *scriptedGroup) Next(ctx context.Context) (*segmentio.Generation, error) {
	return group.next(ctx, group.calls.Add(1))
}

func (group *scriptedGroup) Close() error {
	group.closed.Store(true)
	return nil
}

func TestConsumerGroupRunnerRetriesTemporaryCoordinatorError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	group := &scriptedGroup{
		next: func(ctx context.Context, call int32) (*segmentio.Generation, error) {
			if call == 1 {
				return nil, fmt.Errorf("join group: %w", segmentio.NotCoordinatorForGroup)
			}
			cancel()
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	runner := &consumerGroupRunner{group: group}

	err := runner.Run(ctx, func(context.Context, segmentio.Message, recordCommitter) error {
		t.Fatal("temporary coordinator error must not admit a record")
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, int32(2), group.calls.Load())
	require.True(t, group.closed.Load())
}

func TestConsumerGroupRunnerReturnsPermanentGroupError(t *testing.T) {
	permanent := errors.New("permanent group failure")
	group := &scriptedGroup{
		next: func(context.Context, int32) (*segmentio.Generation, error) {
			return nil, permanent
		},
	}
	runner := &consumerGroupRunner{group: group}

	err := runner.Run(t.Context(), func(context.Context, segmentio.Message, recordCommitter) error {
		t.Fatal("permanent group error must not admit a record")
		return nil
	})

	require.ErrorIs(t, err, permanent)
	require.Equal(t, int32(1), group.calls.Load())
	require.True(t, group.closed.Load())
}
