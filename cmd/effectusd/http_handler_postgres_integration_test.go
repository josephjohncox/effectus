//go:build integration

package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/effectus/effectus-go/loader"
	effectusruntime "github.com/effectus/effectus-go/runtime"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/unified"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestHandleFactsCheckedPostgresRestartReplay(t *testing.T) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = os.Getenv("POSTGRES_DSN")
	}
	if dsn == "" {
		t.Skip("DB_DSN or POSTGRES_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.PingContext(t.Context()))
	require.NoError(t, schema.MigrateSagaV2(t.Context(), db))

	key := fmt.Sprintf("handler-restart-%d", time.Now().UnixNano())
	body := `{"universe":"projection","namespace":"restart-tenant","facts":{"ready":true}}`
	firstRuntime := newPostgresHandlerRuntime(t, db, "handler-first")
	firstState := newServerState(&unified.Bundle{Name: "orders", Version: "1"}, nil, newMemoryFactStore(factStoreConfig{}), factStoreConfig{}, apiAuth{mode: "disabled"}, nil, nil, nil, nil, nil, false, nil, false, nil, nil)
	firstState.SetCheckedEngine(firstRuntime.Engine())
	first := postCheckedHandler(firstState, key, body)
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	require.NoError(t, firstRuntime.Close())

	secondRuntime := newPostgresHandlerRuntime(t, db, "handler-second")
	defer secondRuntime.Close()
	secondState := newServerState(&unified.Bundle{Name: "orders", Version: "1"}, nil, newMemoryFactStore(factStoreConfig{}), factStoreConfig{}, apiAuth{mode: "disabled"}, nil, nil, nil, nil, nil, false, nil, false, nil, nil)
	secondState.SetCheckedEngine(secondRuntime.Engine())
	replay := postCheckedHandler(secondState, key, body)
	require.Equal(t, http.StatusAccepted, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
}

func newPostgresHandlerRuntime(t *testing.T, db *sql.DB, owner string) *effectusruntime.ExecutionRuntime {
	t.Helper()
	store, err := schema.NewPostgresOutboxStore(db)
	require.NoError(t, err)
	execution := effectusruntime.NewExecutionRuntime()
	execution.EnableLegacyExecutionForCompatibility()
	execution.RegisterExtensionLoader(loader.NewStaticSourceLoader("handler", "handler.effx", []byte(`flow "empty" priority 1 { when {} steps {} }`)))
	require.NoError(t, execution.CompileAndValidate(t.Context()))
	require.NoError(t, execution.ConfigureDurableWorkflowExecution(store, nil, schema.DispatcherOptions{Owner: owner}))
	require.NoError(t, execution.ConfigureExecutionLedger(store, effectusruntime.NewManifestArtifactResolver()))
	return execution
}

func postCheckedHandler(state *serverState, key, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/facts", strings.NewReader(body))
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	state.handleFacts(response, request)
	return response
}
