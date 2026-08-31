package amqp

import (
	"context"
	"testing"
	"time"

	rabbit "github.com/rabbitmq/amqp091-go"

	"github.com/josephjohncox/effectus/adapters"
)

type testAcknowledger struct{ acknowledgements int }

func (acknowledger *testAcknowledger) Ack(uint64, bool) error {
	acknowledger.acknowledgements++
	return nil
}
func (*testAcknowledger) Nack(uint64, bool, bool) error { return nil }
func (*testAcknowledger) Reject(uint64, bool) error     { return nil }

func TestManualAckWaitsForDurableCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &Source{config: &Config{SourceID: "test", Format: "json", SchemaName: "event", SchemaVersion: "v1"}, metrics: adapters.GetGlobalMetrics(), ctx: ctx}
	deliveries := make(chan rabbit.Delivery, 1)
	facts := make(chan *adapters.TypedFact, 1)
	done := make(chan struct{})
	acknowledger := &testAcknowledger{}
	deliveries <- rabbit.Delivery{Acknowledger: acknowledger, DeliveryTag: 7, Body: []byte(`{"id":1}`), ContentType: "application/json"}
	close(deliveries)
	go source.processMessages(ctx, deliveries, facts, done)
	fact := <-facts
	if acknowledger.acknowledgements != 0 {
		t.Fatal("delivery acknowledged before durable callback")
	}
	if fact.Acknowledge == nil {
		t.Fatal("manual-ack delivery has no callback")
	}
	if err := fact.Acknowledge(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := fact.Acknowledge(t.Context()); err != nil {
		t.Fatal(err)
	}
	if acknowledger.acknowledgements != 1 {
		t.Fatalf("broker acknowledgements = %d, want 1", acknowledger.acknowledgements)
	}
	<-done
}

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
