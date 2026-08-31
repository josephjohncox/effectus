package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/go-mysql-org/go-mysql/replication"

	"github.com/josephjohncox/effectus/adapters"
)

func TestDurableCheckpointResumesAcknowledgedCoordinate(t *testing.T) {
	path := t.TempDir() + "/checkpoint.json"
	writer := &CDCSource{config: &CDCConfig{CheckpointPath: path}}
	requireNoError := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	requireNoError(writer.persistCheckpoint(t.Context(), cdcCheckpoint{Binlog: "mysql-bin.000007", Pos: 321}))
	reader := &CDCSource{config: &CDCConfig{CheckpointPath: path}}
	requireNoError(reader.initializeCheckpoint(t.Context()))
	if reader.config.StartFile != "mysql-bin.000007" || reader.config.StartPos != 321 {
		t.Fatalf("resumed coordinate = %s:%d", reader.config.StartFile, reader.config.StartPos)
	}
}

func TestEmitChangeBlockedChannelReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &CDCSource{
		config:  &CDCConfig{SourceID: "test", SchemaMapping: map[string]string{}},
		metrics: adapters.GetGlobalMetrics(), currentBinlog: "mysql-bin.000001",
	}
	out := make(chan *adapters.TypedFact)
	done := make(chan error, 1)
	go func() {
		done <- source.emitChange(ctx, out, "INSERT", "db", "events", nil, map[string]interface{}{"id": 1}, &replication.EventHeader{LogPos: 10, Timestamp: uint32(time.Now().Unix())}, "mysql-bin.000001", nil)
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
