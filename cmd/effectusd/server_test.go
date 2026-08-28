package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/effectus/effectus-go/adapters"
	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/pathutil"
	effectusruntime "github.com/effectus/effectus-go/runtime"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/effectus/effectus-go/unified"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPServerConfiguresLimitsAndReportsBindFailure(t *testing.T) {
	state := newServerState(nil, nil, nil, factStoreConfig{}, apiAuth{mode: "disabled"}, nil, nil, nil, nil, nil, false, nil, false, nil, nil)
	server, listener, err := newHTTPServer("127.0.0.1:0", state)
	require.NoError(t, err)
	require.NotZero(t, server.ReadHeaderTimeout)
	require.NotZero(t, server.ReadTimeout)
	require.NotZero(t, server.WriteTimeout)
	require.NotZero(t, server.IdleTimeout)
	require.Positive(t, server.MaxHeaderBytes)
	require.NoError(t, listener.Close())

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()
	server, listener, err = newHTTPServer(occupied.Addr().String(), state)
	require.Error(t, err)
	require.Nil(t, server)
	require.Nil(t, listener)
}

func TestRequestBodyLimitBoundsReads(t *testing.T) {
	handler := withRequestBodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}), 4)

	request := httptest.NewRequest(http.MethodPost, "/api/facts", strings.NewReader("12345"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
}

func TestIngestFactsReturnsQueueFull(t *testing.T) {
	queue := make(chan factEnvelope, 1)
	queue <- factEnvelope{Universe: "occupied"}
	store := newMemoryFactStore(factStoreConfig{})
	state := &serverState{factCh: queue, factStore: store}

	err := state.IngestFacts(factEnvelope{Facts: map[string]interface{}{"ready": true}})
	require.ErrorIs(t, err, errFactQueueFull)
	require.Len(t, queue, 1)
	_, mutated := store.Snapshot("default")
	require.False(t, mutated, "a rejected request must not mutate fact state")
}

// TestHandleFactsReportsBackpressure covers the compatibility-only embedded
// queue. Production effectusd installs the checked durable engine.
func TestHandleFactsReportsBackpressure(t *testing.T) {
	queue := make(chan factEnvelope, 1)
	queue <- factEnvelope{Universe: "occupied"}
	state := &serverState{factCh: queue}

	request := httptest.NewRequest(http.MethodPost, "/facts", strings.NewReader(`{"facts":{"ready":true}}`))
	response := httptest.NewRecorder()
	state.handleFacts(response, request)

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Equal(t, "1", response.Header().Get("Retry-After"))
	require.Contains(t, response.Body.String(), "fact execution queue is full")
}

// TestHandleFactsEnqueuesAcceptedWork covers the compatibility-only embedded queue.
func TestHandleFactsEnqueuesAcceptedWork(t *testing.T) {
	queue := make(chan factEnvelope, 1)
	state := &serverState{factCh: queue}

	request := httptest.NewRequest(http.MethodPost, "/facts", strings.NewReader(`{"facts":{"ready":true}}`))
	response := httptest.NewRecorder()
	state.handleFacts(response, request)

	require.Equal(t, http.StatusAccepted, response.Code)
	require.Len(t, queue, 1)
	envelope := <-queue
	require.Equal(t, "default", envelope.Universe)
	require.False(t, envelope.Received.IsZero())
	require.Equal(t, true, envelope.Facts["ready"])
}

func TestStatusReportsDistinctBundleAndEngineDigests(t *testing.T) {
	execution := effectusruntime.NewExecutionRuntime()
	execution.EnableLegacyExecutionForCompatibility()
	require.NoError(t, execution.ConfigureGenerationMetadata(effectusruntime.GenerationMetadata{BundleDigest: "bundle-digest"}))
	execution.RegisterExtensionLoader(loader.NewStaticSourceLoader("handler", "handler.effx", []byte(`flow "empty" priority 1 { when {} steps {} }`)))
	require.NoError(t, execution.CompileAndValidate(t.Context()))
	t.Cleanup(func() { require.NoError(t, execution.Close()) })
	state := newServerState(&unified.Bundle{Name: "orders", Version: "1"}, nil, nil, factStoreConfig{}, apiAuth{mode: "disabled"}, nil, nil, nil, nil, nil, false, nil, false, nil, nil)
	artifactDigest := state.generation.bundleDigest
	state.SetCheckedEngine(execution.Engine())
	response := httptest.NewRecorder()
	state.handleStatus(response, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var status statusResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	require.Equal(t, artifactDigest, status.ArtifactDigest)
	require.Equal(t, "bundle-digest", status.BundleDigest)
	require.Equal(t, "bundle-digest", status.BundleGenerationDigest)
	require.NotEmpty(t, status.EngineGenerationDigest)
	require.NotEqual(t, status.BundleDigest, status.EngineGenerationDigest)
}

func TestHandleFactsCheckedIdempotencyAndNamespace(t *testing.T) {
	execution := newCheckedHandlerRuntime(t)
	store := newMemoryFactStore(factStoreConfig{})
	state := newServerState(&unified.Bundle{Name: "orders", Version: "1"}, nil, store, factStoreConfig{}, apiAuth{mode: "disabled"}, nil, nil, nil, nil, nil, false, nil, false, nil, nil)
	state.SetCheckedEngine(execution.Engine())

	post := func(key, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/facts", strings.NewReader(body))
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		response := httptest.NewRecorder()
		state.handleFacts(response, request)
		return response
	}

	missing := post("", `{"universe":"projection","namespace":"tenant-a","facts":{"ready":true}}`)
	require.Equal(t, http.StatusBadRequest, missing.Code)

	first := post("request-1", `{"universe":"projection","namespace":"tenant-a","facts":{"ready":true}}`)
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	var firstBody map[string]string
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstBody))
	require.Equal(t, schema.StableExecutionID("tenant-a", "request-1", "orders", "1"), firstBody["execution_id"])

	replay := post("request-1", `{"universe":"projection","namespace":"tenant-a","facts":{"ready":true}}`)
	require.Equal(t, http.StatusAccepted, replay.Code, replay.Body.String())
	var replayBody map[string]string
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayBody))
	require.Equal(t, firstBody["execution_id"], replayBody["execution_id"])

	conflict := post("request-1", `{"universe":"projection","namespace":"tenant-a","facts":{"ready":false}}`)
	require.Equal(t, http.StatusConflict, conflict.Code)
	facts, ok := store.Snapshot("projection")
	require.True(t, ok)
	require.Equal(t, true, facts["ready"])
	_, tenantProjection := store.Snapshot("tenant-a")
	require.False(t, tenantProjection, "namespace must not replace the projection universe")

	fallback := post("request-2", `{"universe":"legacy-tenant","facts":{"ready":true}}`)
	require.Equal(t, http.StatusAccepted, fallback.Code, fallback.Body.String())
}

type projectionFailStore struct{}

func (projectionFailStore) Update(string, map[string]interface{}) error {
	return errors.New("projection unavailable")
}
func (projectionFailStore) Snapshot(string) (map[string]interface{}, bool) { return nil, false }
func (projectionFailStore) Summaries() []universeSummary                   { return nil }
func (projectionFailStore) StoreType() string                              { return "failing" }

func TestHandleFactsCheckedProjectionFailureAfterAcceptance(t *testing.T) {
	execution := newCheckedHandlerRuntime(t)
	state := newServerState(&unified.Bundle{Name: "orders", Version: "1"}, nil, projectionFailStore{}, factStoreConfig{}, apiAuth{mode: "disabled"}, nil, nil, nil, nil, nil, false, nil, false, nil, nil)
	state.SetCheckedEngine(execution.Engine())
	request := httptest.NewRequest(http.MethodPost, "/api/facts", strings.NewReader(`{"universe":"projection","namespace":"tenant-a","facts":{"ready":true}}`))
	request.Header.Set("Idempotency-Key", "projection-failure")
	response := httptest.NewRecorder()
	state.handleFacts(response, request)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Contains(t, response.Body.String(), "accepted but local projection failed")

	// The same durable request remains a replay even though the projection failed.
	retry := httptest.NewRequest(http.MethodPost, "/api/facts", strings.NewReader(`{"universe":"projection","namespace":"tenant-a","facts":{"ready":true}}`))
	retry.Header.Set("Idempotency-Key", "projection-failure")
	retryResponse := httptest.NewRecorder()
	state.handleFacts(retryResponse, retry)
	require.Equal(t, http.StatusInternalServerError, retryResponse.Code)
}

func newCheckedHandlerRuntime(t *testing.T) *effectusruntime.ExecutionRuntime {
	t.Helper()
	execution := effectusruntime.NewExecutionRuntime()
	execution.EnableLegacyExecutionForCompatibility()
	execution.RegisterExtensionLoader(loader.NewStaticSourceLoader("handler", "handler.effx", []byte(`flow "empty" priority 1 { when {} steps {} }`)))
	require.NoError(t, execution.CompileAndValidate(t.Context()))
	require.NoError(t, execution.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "handler-test"}))
	t.Cleanup(func() { require.NoError(t, execution.Close()) })
	return execution
}

func TestExecutionSnapshotKeepsBundleAndTypesCoherent(t *testing.T) {
	state := newServerState(
		&unified.Bundle{Name: "initial"},
		nil,
		nil,
		factStoreConfig{},
		apiAuth{},
		nil,
		nil,
		types.NewTypeSystem(),
		nil,
		nil,
		false,
		nil,
		false,
		nil,
		nil,
	)

	const iterations = 100
	failures := make(chan string, iterations)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		for i := 0; i < iterations; i++ {
			path := "generation." + strconv.Itoa(i)
			state.SetBundle(&unified.Bundle{
				Name:      strconv.Itoa(i),
				FactTypes: []unified.FactTypeSummary{{Path: path, Type: "string"}},
			})
		}
	}()
	go func() {
		defer wait.Done()
		for i := 0; i < iterations; i++ {
			bundle, typeSystem := state.executionSnapshot()
			if bundle == nil || typeSystem == nil {
				failures <- "snapshot contained nil state"
				continue
			}
			for _, factType := range bundle.FactTypes {
				if _, err := typeSystem.GetFactType(factType.Path); err != nil {
					failures <- factType.Path + ": " + err.Error()
				}
			}
		}
	}()
	wait.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}

func TestGenerationActivationRejectsStaleCandidate(t *testing.T) {
	state := newServerState(&unified.Bundle{Name: "initial"}, nil, nil, factStoreConfig{}, apiAuth{}, nil, nil, types.NewTypeSystem(), nil, verb.NewRegistry(nil), false, nil, false, nil, nil)
	base := state.generationSnapshot()

	state.SetBundle(&unified.Bundle{Name: "newer"})
	err := state.ActivateBundle(&unified.Bundle{Name: "stale"}, base.id)
	require.ErrorIs(t, err, errGenerationConflict)
	require.Equal(t, "newer", state.Bundle().Name)
}

func TestSchemaReloadKeepsLastKnownGoodOnFailure(t *testing.T) {
	base := types.NewTypeSystem()
	base.RegisterFactType("stable.fact", types.NewStringType())
	state := newServerState(
		&unified.Bundle{Name: "stable"},
		nil,
		nil,
		factStoreConfig{},
		apiAuth{},
		nil,
		nil,
		base,
		nil,
		nil,
		false,
		nil,
		false,
		nil,
		nil,
	)
	_, before := state.executionSnapshot()

	err := reloadSchemaSources(context.Background(), state, []adapters.SchemaSourceConfig{{
		Name: "invalid",
		Type: "unsupported-source-type",
	}}, false)
	require.Error(t, err)

	_, after := state.executionSnapshot()
	require.Same(t, before, after)
	_, err = after.GetFactType("stable.fact")
	require.NoError(t, err)
}

func TestExtensionReloadKeepsLastKnownGoodRegistryOnFailure(t *testing.T) {
	base := types.NewTypeSystem()
	original := verb.NewRegistry(base)
	require.NoError(t, original.RegisterVerb(&verb.Spec{Name: "stable", ReturnType: "any"}))
	state := newServerState(
		&unified.Bundle{Name: "stable"},
		nil,
		nil,
		factStoreConfig{},
		apiAuth{},
		nil,
		nil,
		base,
		nil,
		original,
		false,
		nil,
		false,
		nil,
		nil,
	)

	err := reloadVerbsAndExtensions(state, []string{filepath.Join(t.TempDir(), "missing")}, nil)
	require.Error(t, err)
	_, current := state.compilerSnapshot()
	require.Same(t, original, current)
	_, exists := current.GetVerb("stable")
	require.True(t, exists)
}

func TestVerbRegistrySwapPublishesCoherentExecutionTypes(t *testing.T) {
	base := types.NewTypeSystem()
	state := newServerState(&unified.Bundle{Name: "bundle"}, nil, nil, factStoreConfig{}, apiAuth{}, nil, nil, base, nil, verb.NewRegistry(base), false, nil, false, nil, nil)
	candidate := verb.NewRegistry(base)
	require.NoError(t, candidate.RegisterVerb(&verb.Spec{Name: "replacement", ReturnType: "any"}))

	state.SetVerbRegistry(candidate)
	_, executionTypes, current := state.executionRuntimeSnapshot()
	require.Same(t, candidate, current)
	_, err := executionTypes.GetVerbSpec("replacement")
	require.NoError(t, err)
}

func TestValidateFactSourceAcceptsKafkaAndRejectsUnsupportedContracts(t *testing.T) {
	originalSource := *factSource
	originalBrokers := *kafkaBrokers
	originalTopic := *kafkaTopic
	originalGroup := *kafkaConsumerGroup
	originalCluster := *kafkaClusterNamespace
	originalContract := *kafkaAckContract
	originalLedger := *kafkaDeliveryLedger
	originalDSN := *postgresDSN
	t.Cleanup(func() {
		*factSource = originalSource
		*kafkaBrokers = originalBrokers
		*kafkaTopic = originalTopic
		*kafkaConsumerGroup = originalGroup
		*kafkaClusterNamespace = originalCluster
		*kafkaAckContract = originalContract
		*kafkaDeliveryLedger = originalLedger
		*postgresDSN = originalDSN
	})

	*factSource = "http"
	require.NoError(t, validateFactSource())

	*factSource = "kafka"
	*kafkaBrokers = "broker:9092"
	*kafkaTopic = "facts"
	*kafkaConsumerGroup = "effectusd-test"
	*kafkaClusterNamespace = "test"
	*kafkaAckContract = "completed_processing"
	*kafkaDeliveryLedger = filepath.Join(t.TempDir(), "deliveries.jsonl")
	*postgresDSN = "postgres://configured"
	require.NoError(t, validateFactSource())

	*kafkaAckContract = "durable_acceptance"
	require.NoError(t, validateFactSource())

	*factSource = "unknown"
	require.ErrorContains(t, validateFactSource(), "unsupported fact source")
}

func TestAPIAuthenticationDoesNotReadTokensFromURLs(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/status?token=secret", nil)
	require.Empty(t, extractToken(request))
	request.Header.Set("Authorization", "Bearer secret")
	require.Equal(t, "secret", extractToken(request))
}

func TestExecutableGenerationDigestIncludesVerbSource(t *testing.T) {
	bundle := &unified.Bundle{Name: "orders", Version: "1"}
	first := verb.NewRegistry(types.NewTypeSystem())
	second := verb.NewRegistry(types.NewTypeSystem())
	for _, registry := range []*verb.Registry{first, second} {
		require.NoError(t, registry.RegisterVerb(&verb.Spec{Name: "charge", ArgTypes: map[string]string{}, ReturnType: "void"}))
	}
	first.SetVerbSource("charge", verb.SourceInfo{Type: verb.SourceHTTP, Ref: "https://one.example"})
	second.SetVerbSource("charge", verb.SourceInfo{Type: verb.SourceHTTP, Ref: "https://two.example"})
	firstDigest, err := executableGenerationDigest(bundle, first)
	require.NoError(t, err)
	secondDigest, err := executableGenerationDigest(bundle, second)
	require.NoError(t, err)
	require.NotEqual(t, firstDigest, secondDigest)
}

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

func TestFileFactStoreSerializesConcurrentUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "facts.json")
	store, err := newFileFactStore(path, factStoreConfig{defaultStrategy: pathutil.MergeLast})
	require.NoError(t, err)

	const updateCount = 32
	errors := make(chan error, updateCount)
	var wait sync.WaitGroup
	for i := 0; i < updateCount; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			universe := "universe-" + strconv.Itoa(index)
			errors <- store.Update(universe, map[string]interface{}{"index": index})
		}(i)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	reloaded, err := newFileFactStore(path, factStoreConfig{defaultStrategy: pathutil.MergeLast})
	require.NoError(t, err)
	for i := 0; i < updateCount; i++ {
		universe := "universe-" + strconv.Itoa(i)
		snapshot, ok := reloaded.Snapshot(universe)
		require.True(t, ok, "missing %s after reload", universe)
		require.EqualValues(t, i, snapshot["index"])
	}

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	leftovers, err := filepath.Glob(filepath.Join(dir, ".facts.json.tmp-*"))
	require.NoError(t, err)
	require.Empty(t, leftovers)
}

func TestFileFactStoreRollsBackMemoryWhenPersistFails(t *testing.T) {
	dir := t.TempDir()
	store, err := newFileFactStore(filepath.Join(dir, "facts.json"), factStoreConfig{defaultStrategy: pathutil.MergeLast})
	require.NoError(t, err)

	// Renaming a temporary file over an existing directory must fail.
	store.path = dir
	err = store.Update("prod", map[string]interface{}{"ready": true})
	require.Error(t, err)
	_, ok := store.Snapshot("prod")
	require.False(t, ok)
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

func TestBuildAPIAuthRejectsIncompleteConfiguration(t *testing.T) {
	_, err := buildAPIAuth("token", "", "")
	require.ErrorContains(t, err, "requires at least one configured token")
	_, err = buildAPIAuth("unknown", "token", "")
	require.ErrorContains(t, err, "unsupported API authentication mode")
}

type kafkaReadinessFunc func() error

func (function kafkaReadinessFunc) Ready() error { return function() }

func TestReadyEndpointIncludesKafkaConsumerState(t *testing.T) {
	typeSystem := types.NewTypeSystem()
	state := newServerState(&unified.Bundle{Name: "bundle", Version: "1"}, nil, nil, factStoreConfig{}, apiAuth{}, nil, nil, typeSystem, nil, verb.NewRegistry(typeSystem), false, nil, false, nil, nil)
	state.SetPhase(phaseRunning)
	state.SetKafkaSource(kafkaReadinessFunc(func() error { return errors.New("commit coordinator unavailable") }))
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	state.handleReady(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), "commit coordinator unavailable")
}

func TestHealthAndReadyEndpoints(t *testing.T) {
	auth, err := buildAPIAuth("token", "test-token", "")
	require.NoError(t, err)

	typeSystem := types.NewTypeSystem()
	state := newServerState(nil, nil, nil, factStoreConfig{}, auth, nil, nil, typeSystem, nil, verb.NewRegistry(typeSystem), false, nil, false, nil, nil)

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
	state.SetPhase(phaseRunning)
	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)

	var ready map[string]string
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &ready))
	require.Equal(t, "ready", ready["status"])
	require.Equal(t, "demo", ready["bundle"])

	state.SetPhase(phaseDraining)
	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

func TestRulesHotloadRequiresEnable(t *testing.T) {
	auth, err := buildAPIAuth("disabled", "", "")
	require.NoError(t, err)

	bundle := &unified.Bundle{Name: "demo", Version: "1.0.0"}
	state := newServerState(bundle, nil, nil, factStoreConfig{}, auth, nil, nil, nil, nil, nil, false, nil, false, nil, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/rules/hotload", state.handleRuleHotload)
	handler := state.withAPIMiddleware(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/rules/hotload", strings.NewReader(`{"content":"rule \"demo\" { when { true } then { Noop() } }"}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusForbidden, resp.Code)
}

func TestRulesHotloadAppliesBundle(t *testing.T) {
	auth, err := buildAPIAuth("disabled", "", "")
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

	state := newServerState(bundle, nil, nil, factStoreConfig{}, auth, nil, nil, types.NewTypeSystem(), nil, nil, true, nil, false, nil, nil)

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
