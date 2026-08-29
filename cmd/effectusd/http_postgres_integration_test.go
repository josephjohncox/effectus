//go:build integration

package main

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/effectus/effectus-go/schema"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestHTTPPostgresRestartReplay(t *testing.T) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		t.Skip("DB_DSN is required")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, schema.MigrateSagaV2(t.Context(), db))
	store, err := schema.NewPostgresOutboxStore(db)
	require.NoError(t, err)
	key := fmt.Sprintf("http-restart-%d", time.Now().UnixNano())
	body := `{"universe":"restart-tenant","facts":{"id":"42"}}`

	projection := &failOnceFactStore{data: make(map[string]map[string]interface{})}
	firstState, firstRuntime := newCheckedHTTPState(t, projection)
	require.NoError(t, firstRuntime.ConfigureDurableWorkflowExecution(store, nil, schema.DispatcherOptions{Owner: "http-restart-first"}))
	first := checkedFactRequest(firstState, key, body)
	require.Equal(t, 202, first.Code, first.Body.String())
	require.NoError(t, firstRuntime.Close())

	secondState, secondRuntime := newCheckedHTTPState(t, projection)
	defer secondRuntime.Close()
	require.NoError(t, secondRuntime.ConfigureDurableWorkflowExecution(store, nil, schema.DispatcherOptions{Owner: "http-restart-second"}))
	replay := checkedFactRequest(secondState, key, body)
	require.Equal(t, 202, replay.Code, replay.Body.String())
	require.Equal(t, first.Body.String(), replay.Body.String())
}
