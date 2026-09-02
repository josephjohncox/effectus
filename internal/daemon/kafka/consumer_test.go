package kafka

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	segmentio "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

type scriptedGroup struct {
	next   func(context.Context, int32) (*segmentio.Generation, error)
	calls  atomic.Int32
	closed atomic.Bool
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
