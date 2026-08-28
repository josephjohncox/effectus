package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/go-mysql-org/go-mysql/replication"

	"github.com/effectus/effectus-go/adapters"
)

func TestEmitChangeBlockedChannelReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &CDCSource{
		config:  &CDCConfig{SourceID: "test", SchemaMapping: map[string]string{}},
		metrics: adapters.GetGlobalMetrics(), currentBinlog: "mysql-bin.000001",
	}
	out := make(chan *adapters.TypedFact)
	done := make(chan error, 1)
	go func() {
		done <- source.emitChange(ctx, out, "INSERT", "db", "events", nil, map[string]interface{}{"id": 1}, &replication.EventHeader{LogPos: 10, Timestamp: uint32(time.Now().Unix())})
	}()
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
