package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/parquet-go/parquet-go"
)

type Event struct {
	EventID    string  `parquet:"name=event_id"`
	UserID     string  `parquet:"name=user_id"`
	EventType  string  `parquet:"name=event_type"`
	OccurredAt string  `parquet:"name=occurred_at"`
	Amount     float64 `parquet:"name=amount"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: seed-parquet.go <output-path>")
		os.Exit(1)
	}

	outputPath := os.Args[1]
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create dir: %v\n", err)
		os.Exit(1)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	writer := parquet.NewGenericWriter[Event](file)

	now := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	rows := []Event{
		{EventID: "evt-101", UserID: "user-1", EventType: "signup", OccurredAt: now.Format(time.RFC3339), Amount: 0},
		{EventID: "evt-102", UserID: "user-2", EventType: "purchase", OccurredAt: now.Add(2 * time.Hour).Format(time.RFC3339), Amount: 42.25},
		{EventID: "evt-103", UserID: "user-1", EventType: "logout", OccurredAt: now.Add(3 * time.Hour).Format(time.RFC3339), Amount: 0},
	}

	if _, err := writer.Write(rows); err != nil {
		fmt.Fprintf(os.Stderr, "write parquet: %v\n", err)
		os.Exit(1)
	}
	if err := writer.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close writer: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %d rows to %s\n", len(rows), outputPath)
}
