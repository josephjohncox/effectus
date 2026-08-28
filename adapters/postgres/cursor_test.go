package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/effectus/effectus-go/adapters"
)

func TestPollerRejectsTimestampWithoutTieBreak(t *testing.T) {
	_, err := NewPostgresPollerSource("test", PollerConfig{
		ConnectionString: "postgres://unused", Query: "SELECT * FROM events", TimestampColumn: "created_at",
	})
	if err == nil || !strings.Contains(err.Error(), "tie_break_column") {
		t.Fatalf("error = %v, want tie_break_column validation", err)
	}
}

func TestPollerBuildsDeterministicTuplePagesFromZero(t *testing.T) {
	p, err := NewPostgresPollerSource("test", PollerConfig{
		ConnectionString: "postgres://unused", Query: "SELECT id, created_at FROM events;",
		TimestampColumn: "created_at", TieBreakColumn: "id", MaxRows: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	query, args := p.buildQuery()
	if strings.Contains(query, "WHERE") || !strings.Contains(query, `ORDER BY "created_at", "id" LIMIT $1`) {
		t.Fatalf("initial query is not an ordered zero-cursor page: %s", query)
	}
	if len(args) != 1 || args[0] != 25 {
		t.Fatalf("initial args = %#v", args)
	}

	p.cursor = pollCursor{timestamp: time.Unix(10, 0), tieBreak: int64(7), set: true}
	query, args = p.buildQuery()
	if !strings.Contains(query, `"created_at" > $1`) || !strings.Contains(query, `"id" > $2`) || !strings.Contains(query, "LIMIT $3") {
		t.Fatalf("tuple cursor query = %s", query)
	}
	if len(args) != 3 || args[1] != int64(7) {
		t.Fatalf("cursor args = %#v", args)
	}
}

func TestCDCBlockedHandoffReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &CDCSource{
		config:      &CDCConfig{SourceID: "test", Operations: []string{"INSERT"}, SchemaMapping: map[string]string{}},
		transformer: NewChangeTransformer(&CDCConfig{SourceID: "test", SchemaMapping: map[string]string{}}),
		metrics:     adapters.GetGlobalMetrics(), ctx: ctx,
		factChan: make(chan *adapters.TypedFact),
	}
	done := make(chan error, 1)
	go func() {
		done <- source.processChangeEvent(&ChangeEvent{
			Operation: "INSERT", Schema: "public", Table: "events", LSN: "0/1", Timestamp: time.Now().UTC(),
		})
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked handoff did not stop after cancellation")
	}
}

func TestParseWal2JSONRejectsMalformedRecord(t *testing.T) {
	if _, err := parseWal2JSON(`{"change":`, 1, "0/1"); err == nil {
		t.Fatal("expected malformed WAL payload to fail the whole record")
	}
}
