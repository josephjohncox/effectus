//go:build integration

package schema

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestConcurrentSagaMigratorsFinish(t *testing.T) {
	db := openSagaIntegrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	errors := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for index := 0; index < 2; index++ {
		go func() {
			ready.Done()
			<-start
			errors <- MigrateSagaV2(ctx, db)
		}()
	}
	ready.Wait()
	close(start)
	for index := 0; index < 2; index++ {
		require.NoError(t, <-errors)
	}
}

func TestValidateSagaSchemaWithRuntimeRoleNoDDL(t *testing.T) {
	admin := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), admin))
	dsn, err := url.Parse(os.Getenv("DB_DSN"))
	if err != nil || dsn.Scheme == "" {
		t.Skip("DB_DSN must be a URL for runtime-role validation")
	}
	role := "effectus_runtime_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	password := "runtime-test-password"
	_, err = admin.Exec(`CREATE ROLE ` + role + ` LOGIN PASSWORD '` + password + `'`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP OWNED BY ` + role)
		_, _ = admin.Exec(`DROP ROLE IF EXISTS ` + role)
	})
	var database string
	require.NoError(t, admin.QueryRow(`SELECT current_database()`).Scan(&database))
	_, err = admin.Exec(`GRANT CONNECT ON DATABASE "` + database + `" TO ` + role)
	require.NoError(t, err)
	_, err = admin.Exec(`GRANT USAGE ON SCHEMA public TO ` + role)
	require.NoError(t, err)
	_, err = admin.Exec(`GRANT SELECT ON effectus_saga_goose_db_version TO ` + role)
	require.NoError(t, err)
	dsn.User = url.UserPassword(role, password)
	runtimeDB, err := sql.Open("pgx", dsn.String())
	require.NoError(t, err)
	defer runtimeDB.Close()
	require.NoError(t, ValidateSagaV2(t.Context(), runtimeDB))
	_, err = runtimeDB.ExecContext(t.Context(), `CREATE TABLE effectus_forbidden_ddl(id integer)`)
	require.Error(t, err)
}

func TestRetentionPruneDryRunBatchAndStateSafety(t *testing.T) {
	db := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	makeExecution := func(state ExecutionState) (string, string) {
		executionID, generation := "prune-"+uuid.NewString(), "generation-"+uuid.NewString()
		admission := testDurableAdmission(executionID, "delivery-"+uuid.NewString(), "payload", generation)
		_, _, err := store.AdmitExecutionAtomic(t.Context(), admission)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE effectus_executions SET state=$2, updated_at=now()-interval '60 days' WHERE execution_id=$1`, executionID, state)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE effectus_fact_applications SET applied_at=now()-interval '60 days' WHERE execution_id=$1`, executionID)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE effectus_fact_snapshots SET created_at=now()-interval '60 days' WHERE execution_id=$1`, executionID)
		require.NoError(t, err)
		t.Cleanup(func() { cleanupExecutionIntegration(t, db, executionID, "", generation) })
		return executionID, generation
	}
	terminalID, _ := makeExecution(ExecutionCompleted)
	nonterminalID, _ := makeExecution(ExecutionRunning)
	blockedID, _ := makeExecution(ExecutionBlockedUnknown)
	poisonID, activeID := "poison-"+uuid.NewString(), "active-"+uuid.NewString()
	_, err = db.Exec(`INSERT INTO effectus_kafka_deliveries(delivery_id, poison_acknowledged, updated_at) VALUES ($1,true,now()-interval '60 days'),($2,false,now()-interval '60 days')`, poisonID, activeID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM effectus_kafka_deliveries WHERE delivery_id IN ($1,$2)`, poisonID, activeID)
	})

	options := PruneOptions{Retention: 30 * 24 * time.Hour, BatchSize: 1, DryRun: true}
	result, err := PruneTerminalRecords(t.Context(), db, options)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Executions)
	require.Equal(t, int64(1), result.KafkaDeliveries)
	_, err = store.GetExecution(t.Context(), terminalID)
	require.NoError(t, err, "dry-run must not mutate")

	options.DryRun = false
	_, err = PruneTerminalRecords(t.Context(), db, options)
	require.NoError(t, err)
	_, err = store.GetExecution(t.Context(), terminalID)
	require.ErrorIs(t, err, ErrExecutionNotFound)
	_, err = store.GetExecution(t.Context(), nonterminalID)
	require.NoError(t, err)
	_, err = store.GetExecution(t.Context(), blockedID)
	require.NoError(t, err)
	var activeCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM effectus_kafka_deliveries WHERE delivery_id=$1`, activeID).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
}
