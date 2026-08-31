//go:build integration

package main

import (
	"database/sql"
	"os"
	"testing"

	kafkaadapter "github.com/josephjohncox/effectus/adapters/kafka"
	"github.com/josephjohncox/effectus/schema"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	segmentio "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

func TestPostgresKafkaDeliveryLedgerSurvivesRestartAndClearsCommittedState(t *testing.T) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		t.Skip("DB_DSN is required")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, schema.MigrateSagaV2(t.Context(), db))
	deliveryID := "kafka/test/facts/0/" + uuid.NewString()
	first := &postgresKafkaDeliveryLedger{db: db}
	failures, err := first.RecordFailure(t.Context(), deliveryID)
	require.NoError(t, err)
	require.Equal(t, 1, failures)
	second := &postgresKafkaDeliveryLedger{db: db}
	failures, err = second.Attempts(t.Context(), deliveryID)
	require.NoError(t, err)
	require.Equal(t, 1, failures)
	require.NoError(t, second.AcknowledgePoison(t.Context(), kafkaadapter.PoisonDisposition{
		DeliveryID: deliveryID, Policy: kafkaadapter.PoisonDLQ, Attempts: failures, Error: "invalid",
		Message: segmentio.Message{Topic: "facts", Partition: 0, Offset: 7},
	}))
	acknowledged, err := first.PoisonAcknowledged(t.Context(), deliveryID)
	require.NoError(t, err)
	require.True(t, acknowledged)
	require.NoError(t, first.ClearAttempts(t.Context(), deliveryID))
	failures, err = second.Attempts(t.Context(), deliveryID)
	require.NoError(t, err)
	require.Zero(t, failures)
	acknowledged, err = second.PoisonAcknowledged(t.Context(), deliveryID)
	require.NoError(t, err)
	require.False(t, acknowledged)
}
