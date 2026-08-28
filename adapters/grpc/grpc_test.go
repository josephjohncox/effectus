package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/effectus/effectus-go/adapters"
)

func TestChannelSendReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan *adapters.TypedFact)
	done := make(chan error, 1)
	go func() { done <- sendFact(ctx, out, &adapters.TypedFact{SchemaName: "event"}) }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked send did not stop")
	}
}
