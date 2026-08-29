package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/effectus/effectus-go/loader"
	effectusruntime "github.com/effectus/effectus-go/runtime"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/effectus/effectus-go/unified"
	"github.com/stretchr/testify/require"
)

type failOnceFactStore struct {
	mu   sync.Mutex
	fail bool
	data map[string]map[string]interface{}
}

func (store *failOnceFactStore) Update(universe string, facts map[string]interface{}) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.fail {
		store.fail = false
		return errors.New("projection failed")
	}
	store.data[universe] = facts
	return nil
}
func (store *failOnceFactStore) Snapshot(universe string) (map[string]interface{}, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.data[universe]
	return value, ok
}
func (*failOnceFactStore) Summaries() []universeSummary { return nil }
func (*failOnceFactStore) StoreType() string            { return "test" }

func newCheckedHTTPState(t *testing.T, store factStore) (*serverState, *effectusruntime.ExecutionRuntime) {
	t.Helper()
	directory := t.TempDir()
	manifest := `{"name":"test","version":"1","verbs":[{"name":"charge","capabilities":["write"],"resources":[{"resource":"payment","capabilities":["write"]}],"argTypes":{"amount":"int"},"requiredArgs":["amount"],"returnType":"void","target":{"type":"noop"}}]}`
	require.NoError(t, os.WriteFile(filepath.Join(directory, "extension.verbs.json"), []byte(manifest), 0o600))
	runtime := effectusruntime.NewExecutionRuntime()
	runtime.EnableLegacyExecutionForCompatibility()
	runtime.RegisterExtensionLoader(loader.NewJSONVerbLoader("test", filepath.Join(directory, "extension.verbs.json")))
	runtime.RegisterExtensionLoader(loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(`flow "charge" priority 1 { when {} steps { charge(amount: 1) } }`)))
	require.NoError(t, runtime.CompileAndValidate(t.Context()))
	require.NoError(t, runtime.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "http-test"}))
	bundle := &unified.Bundle{Name: "orders", Version: "1"}
	state := newServerState(bundle, nil, store, factStoreConfig{}, apiAuth{mode: "disabled"}, nil, nil, types.NewTypeSystem(), nil, verb.NewRegistry(nil), false, nil, false, nil, nil)
	state.SetCheckedEngine(runtime.Engine())
	return state, runtime
}

func checkedFactRequest(state *serverState, key, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/facts", strings.NewReader(body))
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response := httptest.NewRecorder()
	state.handleFacts(response, request)
	return response
}

func TestHandleFactsCheckedAdmissionReplayConflictAndProjection(t *testing.T) {
	store := &failOnceFactStore{data: make(map[string]map[string]interface{})}
	state, runtime := newCheckedHTTPState(t, store)
	defer runtime.Close()
	body := `{"universe":"tenant","facts":{"id":"42"}}`
	require.Equal(t, http.StatusBadRequest, checkedFactRequest(state, "", body).Code)

	store.fail = true
	first := checkedFactRequest(state, "delivery-1", body)
	require.Equal(t, http.StatusInternalServerError, first.Code, "projection failure occurs after durable admission")
	replay := checkedFactRequest(state, "delivery-1", body)
	require.Equal(t, http.StatusAccepted, replay.Code)
	var replayBody map[string]string
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayBody))
	require.NotEmpty(t, replayBody["execution_id"])
	require.NotEmpty(t, replayBody["generation_digest"])

	conflict := checkedFactRequest(state, "delivery-1", `{"universe":"tenant","facts":{"id":"changed"}}`)
	require.Equal(t, http.StatusConflict, conflict.Code)
}
