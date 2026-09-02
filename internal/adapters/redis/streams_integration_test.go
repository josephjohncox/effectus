//go:build integration

package redis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/go-redis/redis/v8"
)

func integrationRedis(t *testing.T) *goredis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set")
	}
	client := goredis.NewClient(&goredis.Options{Addr: addr})
	if err := client.Ping(t.Context()).Err(); err != nil {
		t.Fatalf("Redis unavailable: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestRedisStreamsBackpressureAndExplicitAcknowledgementIntegration(t *testing.T) {
	client := integrationRedis(t)
	run := time.Now().UnixNano()
	stream := fmt.Sprintf("effectus-lossless-%d", run)
	group := fmt.Sprintf("group-%d", run)
	t.Cleanup(func() { client.Del(context.Background(), stream) })

	const count = 150
	for i := 0; i < count; i++ {
		if err := client.XAdd(t.Context(), &goredis.XAddArgs{Stream: stream, Values: map[string]interface{}{"n": i}}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	source, err := NewRedisStreamsSource("integration", StreamsConfig{
		RedisAddr: client.Options().Addr, Streams: []string{stream}, ConsumerGroup: group,
		ConsumerName: "live", BatchSize: 150, BlockTime: 20 * time.Millisecond,
		ClaimMinIdle: time.Second, ClaimInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	facts, err := source.Subscribe(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop(context.Background())

	// Let the adapter fill its channel and block. No message may be dropped.
	time.Sleep(100 * time.Millisecond)
	seen := make(map[string]struct{}, count)
	for len(seen) < count {
		select {
		case fact := <-facts:
			id := fact.Metadata["redis.message_id"]
			seen[id] = struct{}{}
			if err := fact.Acknowledge(ctx); err != nil {
				t.Fatalf("ack %s: %v", id, err)
			}
		case <-ctx.Done():
			t.Fatalf("received %d/%d messages", len(seen), count)
		}
	}
	pending, err := client.XPending(ctx, stream, group).Result()
	if err != nil {
		t.Fatal(err)
	}
	if pending.Count != 0 {
		t.Fatalf("pending count = %d", pending.Count)
	}
}

func TestRedisStreamsRecoversDeadConsumerAndRetriesXACKIntegration(t *testing.T) {
	client := integrationRedis(t)
	run := time.Now().UnixNano()
	stream := fmt.Sprintf("effectus-pel-%d", run)
	group := fmt.Sprintf("group-%d", run)
	t.Cleanup(func() { client.Del(context.Background(), stream) })
	id, err := client.XAdd(t.Context(), &goredis.XAddArgs{Stream: stream, Values: map[string]interface{}{"value": "pending"}}).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.XGroupCreate(t.Context(), stream, group, "0").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.XReadGroup(t.Context(), &goredis.XReadGroupArgs{Group: group, Consumer: "dead", Streams: []string{stream, ">"}, Count: 1}).Result(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)

	source, err := NewRedisStreamsSource("integration", StreamsConfig{
		RedisAddr: client.Options().Addr, Streams: []string{stream}, ConsumerGroup: group,
		ConsumerName: "recovery", BlockTime: 20 * time.Millisecond,
		ClaimMinIdle: 10 * time.Millisecond, ClaimInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := source.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	source.xack = func(ctx context.Context, stream, group, messageID string) (int64, error) {
		if attempts.Add(1) == 1 {
			return 0, errors.New("injected XACK failure")
		}
		return source.client.XAck(ctx, stream, group, messageID).Result()
	}
	facts, err := source.Subscribe(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop(context.Background())
	select {
	case fact := <-facts:
		if got := fact.Metadata["redis.message_id"]; got != id {
			t.Fatalf("recovered id = %s, want %s", got, id)
		}
		pending, err := client.XPending(ctx, stream, group).Result()
		if err != nil {
			t.Fatal(err)
		}
		if pending.Count != 1 {
			t.Fatalf("message was acknowledged before callback; pending=%d", pending.Count)
		}
		time.Sleep(20 * time.Millisecond)
		if err := fact.Acknowledge(ctx); err != nil {
			t.Fatal(err)
		}
		if attempts.Load() < 2 {
			t.Fatal("XACK was not retried")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for recovered pending message")
	}
	pending, err := client.XPending(ctx, stream, group).Result()
	if err != nil {
		t.Fatal(err)
	}
	if pending.Count != 0 {
		t.Fatalf("pending count = %d", pending.Count)
	}
}
