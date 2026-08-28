package amqp

import (
	"context"
	"testing"
	"time"

	rabbit "github.com/rabbitmq/amqp091-go"

	"github.com/effectus/effectus-go/adapters"
)

func TestProducerOwnsChannelCloseOnBlockedSendCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &Source{
		config:  &Config{SourceID: "test", AutoAck: true, Format: "json", SchemaName: "event", SchemaVersion: "v1"},
		metrics: adapters.GetGlobalMetrics(), ctx: ctx,
	}
	deliveries := make(chan rabbit.Delivery)
	facts := make(chan *adapters.TypedFact)
	done := make(chan struct{})
	go source.processMessages(ctx, deliveries, facts, done)
	deliveries <- rabbit.Delivery{Body: []byte(`{"id":1}`), ContentType: "application/json"}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("producer did not stop")
	}
	if _, ok := <-facts; ok {
		t.Fatal("producer did not close output channel")
	}
	select {
	case _, ok := <-facts:
		if ok {
			t.Fatal("closed output produced another value")
		}
	default:
		t.Fatal("closed output channel did not remain closed")
	}
}
