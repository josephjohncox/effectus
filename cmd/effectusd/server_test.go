package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/unified"
	"github.com/stretchr/testify/require"
)

func TestFileFactStorePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "facts.json")

	store, err := newFileFactStore(path, factStoreConfig{defaultStrategy: pathutil.MergeLast})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	input := map[string]interface{}{
		"customer": map[string]interface{}{
			"tier": "gold",
		},
	}
	if err := store.Update("prod", input); err != nil {
		t.Fatalf("update: %v", err)
	}

	store2, err := newFileFactStore(path, factStoreConfig{defaultStrategy: pathutil.MergeLast})
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}

	got, ok := store2.Snapshot("prod")
	if !ok {
		t.Fatalf("expected snapshot")
	}
	if !reflect.DeepEqual(got, input) {
		t.Fatalf("snapshot mismatch: %#v", got)
	}
}

func TestFileFactStoreMergeStrategies(t *testing.T) {
	t.Run("merge last", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "facts.json")
		store, err := newFileFactStore(path, factStoreConfig{defaultStrategy: pathutil.MergeLast})
		if err != nil {
			t.Fatalf("create store: %v", err)
		}
		_ = store.Update("prod", map[string]interface{}{
			"customer": map[string]interface{}{
				"tier": "gold",
				"age":  30,
			},
		})
		if err := store.Update("prod", map[string]interface{}{
			"customer": map[string]interface{}{
				"tier": "platinum",
			},
		}); err != nil {
			t.Fatalf("update: %v", err)
		}
		snapshot, _ := store.Snapshot("prod")
		customer := snapshot["customer"].(map[string]interface{})
		if customer["tier"] != "platinum" {
			t.Fatalf("expected last write to win, got %v", customer["tier"])
		}
		if customer["age"] != 30 {
			t.Fatalf("expected age preserved")
		}
	})

	t.Run("merge first", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "facts.json")
		store, err := newFileFactStore(path, factStoreConfig{defaultStrategy: pathutil.MergeFirst})
		if err != nil {
			t.Fatalf("create store: %v", err)
		}
		_ = store.Update("prod", map[string]interface{}{
			"customer": map[string]interface{}{
				"tier": "gold",
			},
		})
		if err := store.Update("prod", map[string]interface{}{
			"customer": map[string]interface{}{
				"tier": "platinum",
			},
		}); err != nil {
			t.Fatalf("update: %v", err)
		}
		snapshot, _ := store.Snapshot("prod")
		customer := snapshot["customer"].(map[string]interface{})
		if customer["tier"] != "gold" {
			t.Fatalf("expected first write to win, got %v", customer["tier"])
		}
	})

	t.Run("merge error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "facts.json")
		store, err := newFileFactStore(path, factStoreConfig{defaultStrategy: pathutil.MergeError})
		if err != nil {
			t.Fatalf("create store: %v", err)
		}
		_ = store.Update("prod", map[string]interface{}{
			"customer": map[string]interface{}{
				"tier": "gold",
			},
		})
		if err := store.Update("prod", map[string]interface{}{
			"customer": map[string]interface{}{
				"tier": "platinum",
			},
		}); err == nil {
			t.Fatalf("expected merge error")
		}
	})

	t.Run("namespace override", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "facts.json")
		store, err := newFileFactStore(path, factStoreConfig{
			defaultStrategy: pathutil.MergeLast,
			perNamespace: map[string]pathutil.MergeStrategy{
				"customer": pathutil.MergeFirst,
			},
		})
		if err != nil {
			t.Fatalf("create store: %v", err)
		}
		_ = store.Update("prod", map[string]interface{}{
			"customer": map[string]interface{}{
				"tier": "gold",
			},
			"order": map[string]interface{}{
				"total": 120,
			},
		})
		_ = store.Update("prod", map[string]interface{}{
			"customer": map[string]interface{}{
				"tier": "platinum",
			},
			"order": map[string]interface{}{
				"total": 130,
			},
		})
		snapshot, _ := store.Snapshot("prod")
		customer := snapshot["customer"].(map[string]interface{})
		order := snapshot["order"].(map[string]interface{})
		if customer["tier"] != "gold" {
			t.Fatalf("expected namespace override to keep first, got %v", customer["tier"])
		}
		if order["total"] != 130 {
			t.Fatalf("expected default merge last for order")
		}
	})
}

func TestHealthAndReadyEndpoints(t *testing.T) {
	auth, _, err := buildAPIAuth("token", "", "")
	require.NoError(t, err)

	state := newServerState(nil, nil, nil, factStoreConfig{}, auth, nil, nil, nil, nil, nil, false)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", state.handleHealth)
	mux.HandleFunc("/readyz", state.handleReady)
	handler := state.withAPIMiddleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)

	var health map[string]string
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &health))
	require.Equal(t, "ok", health["status"])

	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)

	state.SetBundle(&unified.Bundle{Name: "demo", Version: "1.0.0"})
	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)

	var ready map[string]string
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &ready))
	require.Equal(t, "ready", ready["status"])
	require.Equal(t, "demo", ready["bundle"])
}

func TestRulesHotloadRequiresEnable(t *testing.T) {
	auth, _, err := buildAPIAuth("disabled", "", "")
	require.NoError(t, err)

	bundle := &unified.Bundle{Name: "demo", Version: "1.0.0"}
	state := newServerState(bundle, nil, nil, factStoreConfig{}, auth, nil, nil, nil, nil, nil, false)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/rules/hotload", state.handleRuleHotload)
	handler := state.withAPIMiddleware(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/rules/hotload", strings.NewReader(`{"content":"rule \"demo\" { when { true } then { Noop() } }"}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusForbidden, resp.Code)
}

func TestRulesHotloadAppliesBundle(t *testing.T) {
	auth, _, err := buildAPIAuth("disabled", "", "")
	require.NoError(t, err)

	bundle := &unified.Bundle{
		Name:    "demo",
		Version: "1.0.0",
		FactTypes: []unified.FactTypeSummary{
			{Path: "transaction.amount", Type: "int"},
			{Path: "transaction.id", Type: "string"},
		},
		VerbSpecs: []unified.VerbSpecSummary{
			{
				Name:         "FlagFraud",
				ArgTypes:     map[string]string{"orderId": "string"},
				RequiredArgs: []string{"orderId"},
				ReturnType:   "bool",
			},
		},
	}

	state := newServerState(bundle, nil, nil, factStoreConfig{}, auth, nil, nil, types.NewTypeSystem(), nil, nil, true)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/rules/hotload", state.handleRuleHotload)
	handler := state.withAPIMiddleware(mux)

	rule := `rule "HighValue" priority 1 {
  when { transaction.amount > 100 }
  then { FlagFraud(orderId: transaction.id) }
}`
	req := httptest.NewRequest(http.MethodPost, "/api/rules/hotload", strings.NewReader(`{"path":"rules/high_value.eff","format":"eff","content":`+strconv.Quote(rule)+`, "replace": true}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)

	var payload ruleCheckResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &payload))
	require.True(t, payload.OK, "diagnostics: %+v", payload.Diagnostics)
	require.True(t, payload.Applied)

	updated := state.Bundle()
	require.NotNil(t, updated)
	require.Len(t, updated.Rules, 1)
	require.Len(t, updated.RuleSources, 1)
}
