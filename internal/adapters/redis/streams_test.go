package redis

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/go-redis/redis/v8"

	"github.com/josephjohncox/effectus/internal/adapters"
)

func TestAcknowledgeRetriesCheckedXACK(t *testing.T) {
	source, err := NewRedisStreamsSource("test", StreamsConfig{Streams: []string{"events"}, ConsumerGroup: "group"})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	source.xack = func(context.Context, string, string, string) (int64, error) {
		if calls.Add(1) == 1 {
			return 0, errors.New("one-shot failure")
		}
		return 1, nil
	}
	ack := source.acknowledger("events", "1-0")
	if err := ack(t.Context()); err != nil {
		t.Fatalf("ack failed after retry: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("XACK calls = %d, want 2", got)
	}
	if err := ack(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("successful callback was not idempotent; calls = %d", got)
	}
}

func TestAcknowledgeAcceptsCommitThenResponseError(t *testing.T) {
	source, err := NewRedisStreamsSource("test", StreamsConfig{Streams: []string{"events"}, ConsumerGroup: "group"})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	source.xack = func(context.Context, string, string, string) (int64, error) {
		if calls.Add(1) == 1 {
			return 0, errors.New("response lost after commit")
		}
		return 0, nil
	}
	source.pending = func(context.Context, string, string, string) (bool, error) { return false, nil }
	ack := source.acknowledger("events", "1-0")
	if err := ack(t.Context()); err != nil {
		t.Fatalf("acknowledgement did not verify the committed outcome: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("XACK calls = %d, want 2", calls.Load())
	}
}

func TestDeliveryBlocksUntilAcceptedOrCanceled(t *testing.T) {
	source, err := NewRedisStreamsSource("test", StreamsConfig{Streams: []string{"events"}})
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan *adapters.TypedFact)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- source.deliver(ctx, "events", goredis.XMessage{ID: "1-0", Values: map[string]interface{}{"value": "x"}}, out)
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(time.Second):
		t.Fatal("delivery stayed blocked after cancellation")
	}
}

func TestUncalledAcknowledgementRemainsExplicit(t *testing.T) {
	source, err := NewRedisStreamsSource("test", StreamsConfig{Streams: []string{"events"}})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	source.xack = func(context.Context, string, string, string) (int64, error) { calls.Add(1); return 1, nil }
	out := make(chan *adapters.TypedFact, 1)
	if err := source.deliver(t.Context(), "events", goredis.XMessage{ID: "1-0", Values: map[string]interface{}{}}, out); err != nil {
		t.Fatal(err)
	}
	fact := <-out
	if fact.Acknowledge == nil {
		t.Fatal("missing acknowledgement boundary")
	}
	if calls.Load() != 0 {
		t.Fatal("message was acknowledged before callback")
	}
}
