//go:build integration

package runtime

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func TestPostgresCheckedBytesLiteralAtomicAdmissionAndReplay(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set POSTGRES_DSN to select the PostgreSQL bytes-literal contract")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.PingContext(ctx))
	require.NoError(t, schema.MigrateSagaV2(ctx, db))
	durable, err := schema.NewPostgresOutboxStore(db)
	require.NoError(t, err)

	for _, test := range bytesLiteralCases() {
		t.Run(test.name, func(t *testing.T) {
			environment, checked := checkedBytesLiteral(t, test)
			calls := 0
			executor := languageExecutor(func(_ context.Context, request invocation.Request) invocation.Outcome {
				calls++
				require.Equal(t, test.want, request.Arguments["value"])
				return invocation.Outcome{Class: invocation.OutcomeSuccess}
			})
			executors := map[string]invocation.Executor{"Send": executor}
			engine := languageEngine(t, languageGeneration(t, environment, checked, executors), durable, durable)
			id := "bytes-literal-" + uuid.NewString()
			request := ExecuteRequest{Admission: &Admission{ExecutionID: id, AdmissionID: id, TenantNamespace: "bytes-literal-fixture", Ruleset: "language", Version: "1"}, WaitMode: WaitAccepted}
			accepted, err := engine.Execute(ctx, request)
			require.NoError(t, err)
			require.True(t, accepted.DurablyAccepted)
			require.False(t, accepted.Completed)
			require.Zero(t, calls)
			record, err := durable.GetExecution(ctx, id)
			require.NoError(t, err)
			require.Len(t, record.Plans, 1)
			dispatches, err := durable.ListDispatches(ctx, record.Plans[0].SagaID)
			require.NoError(t, err)
			require.Len(t, dispatches, 1, "atomic admission must persist initial intent before execution")
			initial := dispatches[0]
			wantJSON, wantHash, err := schema.CanonicalJSON(map[string]any{"value": test.want})
			require.NoError(t, err)
			// PostgreSQL jsonb can add whitespace on read. Recanonicalize
			// the stored value, then check both exact JSON and its hash.
			decoded, err := decodeCheckedWorkflowResult(initial.Arguments)
			require.NoError(t, err)
			storedJSON, storedHash, err := schema.CanonicalJSON(decoded)
			require.NoError(t, err)
			require.Equal(t, wantJSON, storedJSON)
			require.Equal(t, wantHash, storedHash)
			require.Equal(t, wantHash, initial.ArgumentHash)
			require.Equal(t, schema.DispatchQueued, initial.State)
			require.NoError(t, engine.Close())

			restarted := languageEngine(t, languageGeneration(t, environment, checked, executors), durable, durable)
			done, err := restarted.Execute(ctx, ExecuteRequest{ResumeExecutionID: id, WaitMode: WaitTerminal})
			require.NoError(t, err)
			require.True(t, done.Completed)
			require.Equal(t, 1, calls)
			completed, err := durable.GetDispatch(ctx, initial.ID)
			require.NoError(t, err)
			require.Equal(t, schema.DispatchSucceeded, completed.State)
			require.Equal(t, initial.Arguments, completed.Arguments)
			require.Equal(t, initial.ArgumentHash, completed.ArgumentHash)
			require.Equal(t, initial.IdempotencyKey, completed.IdempotencyKey)
			for _, replay := range []ExecuteRequest{{ResumeExecutionID: id, WaitMode: WaitTerminal}, request} {
				result, err := restarted.Execute(ctx, replay)
				require.NoError(t, err)
				require.True(t, result.Completed)
				require.Equal(t, id, result.ExecutionID)
			}
			require.Equal(t, 1, calls)
			attempts, err := durable.ListAttempts(ctx, initial.ID)
			require.NoError(t, err)
			require.Len(t, attempts, 1)
			unchanged, err := durable.GetDispatch(ctx, initial.ID)
			require.NoError(t, err)
			require.Equal(t, completed, unchanged)
		})
	}
	// Keep uniquely identified fixture rows. Container/data cleanup is a
	// separate, explicitly authorized operation, not this test's failure path.
}
