package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/unified"
)

type factEnvelope struct {
	Universe  string                 `json:"universe"`
	Facts     map[string]interface{} `json:"facts"`
	Source    string                 `json:"source,omitempty"`
	Received  time.Time              `json:"received_at"`
	Namespace string                 `json:"namespace,omitempty"`
}

type serverState struct {
	mu         sync.RWMutex
	bundle     *unified.Bundle
	startedAt  time.Time
	updatedAt  time.Time
	factStore  factStore
	factConfig factStoreConfig
	factCh     chan<- factEnvelope
	auth       apiAuth
	limiter    *rateLimiter
	acl        *aclMatcher
}

func newServerState(bundle *unified.Bundle, factCh chan<- factEnvelope, store factStore, config factStoreConfig, auth apiAuth, limiter *rateLimiter, acl *aclMatcher) *serverState {
	return &serverState{
		bundle:     bundle,
		startedAt:  time.Now(),
		updatedAt:  time.Now(),
		factStore:  store,
		factConfig: config,
		factCh:     factCh,
		auth:       auth,
		limiter:    limiter,
		acl:        acl,
	}
}

func (s *serverState) SetBundle(bundle *unified.Bundle) {
	s.mu.Lock()
	s.bundle = bundle
	s.updatedAt = time.Now()
	s.mu.Unlock()
}

func (s *serverState) Bundle() *unified.Bundle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bundle
}

func (s *serverState) IngestFacts(env factEnvelope) error {
	if env.Universe == "" {
		env.Universe = "default"
	}
	if env.Received.IsZero() {
		env.Received = time.Now()
	}
	if s.factStore != nil {
		if err := s.factStore.Update(env.Universe, env.Facts); err != nil {
			return err
		}
	}
	if s.factCh != nil {
		select {
		case s.factCh <- env:
		default:
		}
	}
	return nil
}

type apiRole int

const (
	roleRead apiRole = iota
	roleWrite
)

type apiAuth struct {
	mode   string
	tokens map[string]apiRole
}

func (a apiAuth) enabled() bool {
	return strings.ToLower(a.mode) != "disabled"
}

func (a apiAuth) Authorize(r *http.Request, required apiRole) (bool, bool) {
	if !a.enabled() {
		return true, true
	}
	token := extractToken(r)
	if token == "" {
		return false, false
	}
	role, ok := a.tokens[token]
	if !ok {
		return false, true
	}
	if role == roleWrite || required == roleRead {
		return true, true
	}
	return false, true
}

func extractToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	if strings.HasPrefix(auth, "Token ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Token "))
	}
	if token := strings.TrimSpace(r.Header.Get("X-Effectus-Token")); token != "" {
		return token
	}
	if token := strings.TrimSpace(r.Header.Get("X-Effectus-Read-Token")); token != "" {
		return token
	}
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		return token
	}
	return ""
}

type rateLimiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	bucket map[string]*rateBucket
}

type rateBucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(requestsPerMinute int, burst int) *rateLimiter {
	if requestsPerMinute <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = requestsPerMinute
	}
	return &rateLimiter{
		rate:   float64(requestsPerMinute) / 60.0,
		burst:  float64(burst),
		bucket: make(map[string]*rateBucket),
	}
}

func (rl *rateLimiter) Allow(key string) bool {
	if rl == nil {
		return true
	}
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, ok := rl.bucket[key]
	if !ok {
		rl.bucket[key] = &rateBucket{tokens: rl.burst - 1, last: now}
		return true
	}
	elapsed := now.Sub(bucket.last).Seconds()
	bucket.tokens = minFloat(rl.burst, bucket.tokens+(elapsed*rl.rate))
	bucket.last = now
	if bucket.tokens >= 1 {
		bucket.tokens -= 1
		return true
	}
	return false
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func (s *serverState) withAPIMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		if s.limiter != nil && !s.limiter.Allow(clientKey(r)) {
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		required := requiredRoleFor(r)
		if s.acl != nil {
			required = s.acl.requiredRole(r, required)
		}
		if ok, hasToken := s.auth.Authorize(r, required); !ok {
			if !hasToken {
				writeJSONError(w, http.StatusUnauthorized, "missing or invalid token")
				return
			}
			writeJSONError(w, http.StatusForbidden, "insufficient permissions")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func requiredRoleFor(r *http.Request) apiRole {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return roleRead
	}
	if r.URL.Path == "/api/playground/dry-run" {
		return roleRead
	}
	return roleWrite
}

func clientKey(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host := r.RemoteAddr
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			return h
		}
	}
	return host
}

func buildAPIAuth(mode, writeTokens, readTokens string) (apiAuth, string, error) {
	auth := apiAuth{
		mode:   strings.ToLower(strings.TrimSpace(mode)),
		tokens: make(map[string]apiRole),
	}
	if auth.mode == "" {
		auth.mode = "token"
	}
	if !auth.enabled() {
		return auth, "", nil
	}

	addTokens := func(raw string, role apiRole) {
		for _, token := range strings.Split(raw, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			auth.tokens[token] = role
		}
	}

	addTokens(writeTokens, roleWrite)
	addTokens(readTokens, roleRead)

	var generated string
	if len(auth.tokens) == 0 {
		token, err := generateToken()
		if err != nil {
			return apiAuth{}, "", err
		}
		auth.tokens[token] = roleWrite
		generated = token
	}

	return auth, generated, nil
}

func generateToken() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func parseMergeStrategy(input string) (pathutil.MergeStrategy, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "", "first":
		return pathutil.MergeFirst, nil
	case "last":
		return pathutil.MergeLast, nil
	case "error":
		return pathutil.MergeError, nil
	default:
		return "", fmt.Errorf("unknown merge strategy %q", input)
	}
}

func startHTTPServer(ctx context.Context, addr string, state *serverState) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", state.handleUI)
	mux.HandleFunc("/ui", state.handleUI)
	mux.HandleFunc("/healthz", state.handleHealth)
	mux.HandleFunc("/readyz", state.handleReady)
	mux.HandleFunc("/api/status", state.handleStatus)
	mux.HandleFunc("/api/rules", state.handleRules)
	mux.HandleFunc("/api/rules/source", state.handleRuleSources)
	mux.HandleFunc("/api/flows", state.handleFlows)
	mux.HandleFunc("/api/graph", state.handleGraph)
	mux.HandleFunc("/api/verbs", state.handleVerbs)
	mux.HandleFunc("/api/schema", state.handleSchema)
	mux.HandleFunc("/api/playground/dry-run", state.handleDryRun)
	mux.HandleFunc("/api/facts", state.handleFacts)

	server := &http.Server{
		Addr:    addr,
		Handler: state.withAPIMiddleware(mux),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("Starting HTTP server on %s\n", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Printf("HTTP server error: %v\n", err)
	}
	fmt.Println("Shutting down HTTP server")
}

type factStore interface {
	Update(universe string, facts map[string]interface{}) error
	Snapshot(universe string) (map[string]interface{}, bool)
	Summaries() []universeSummary
	StoreType() string
}

type factStoreConfig struct {
	defaultStrategy pathutil.MergeStrategy
	perNamespace    map[string]pathutil.MergeStrategy
	cache           factCacheConfig
}

func (c factStoreConfig) strategyFor(namespace string) pathutil.MergeStrategy {
	if c.perNamespace == nil {
		return c.defaultStrategy
	}
	if strategy, ok := c.perNamespace[namespace]; ok {
		return strategy
	}
	return c.defaultStrategy
}

type factCacheConfig struct {
	policy        string
	maxUniverses  int
	maxNamespaces int
}

func (c factCacheConfig) enabled() bool {
	return c.policy == "lru"
}

type memoryFactStore struct {
	mu        sync.RWMutex
	universes map[string]*universeSnapshot
	config    factStoreConfig
}

type universeSnapshot struct {
	Facts           map[string]interface{}
	UpdatedAt       time.Time
	LastAccess      time.Time
	NamespaceAccess map[string]time.Time
}

type universeSummary struct {
	Universe   string    `json:"universe"`
	Namespaces []string  `json:"namespaces"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func newMemoryFactStore(config factStoreConfig) *memoryFactStore {
	return &memoryFactStore{
		universes: make(map[string]*universeSnapshot),
		config:    config,
	}
}

func (fs *memoryFactStore) Update(universe string, facts map[string]interface{}) error {
	if fs == nil {
		return nil
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if universe == "" {
		universe = "default"
	}
	snapshot, ok := fs.universes[universe]
	if !ok {
		snapshot = &universeSnapshot{
			Facts:           make(map[string]interface{}),
			NamespaceAccess: make(map[string]time.Time),
		}
		fs.universes[universe] = snapshot
	}
	if len(facts) == 0 {
		return nil
	}

	nextFacts := copyMap(snapshot.Facts)
	for namespace, payload := range facts {
		existing, ok := nextFacts[namespace]
		strategy := fs.config.strategyFor(namespace)
		merged, _, err := mergeValue(existing, payload, strategy)
		if err != nil {
			return fmt.Errorf("merge conflict in namespace %q: %w", namespace, err)
		}
		if !ok {
			merged = payload
		}
		nextFacts[namespace] = merged
		snapshot.NamespaceAccess[namespace] = time.Now()
	}
	snapshot.Facts = nextFacts
	snapshot.UpdatedAt = time.Now()
	snapshot.LastAccess = time.Now()
	if fs.config.cache.enabled() {
		applyNamespaceLimit(snapshot, fs.config.cache.maxNamespaces)
		applyUniverseLimit(fs.universes, fs.config.cache.maxUniverses)
	}
	return nil
}

func (fs *memoryFactStore) Snapshot(universe string) (map[string]interface{}, bool) {
	if fs == nil {
		return nil, false
	}
	fs.mu.RLock()
	snapshot, ok := fs.universes[universe]
	if !ok {
		fs.mu.RUnlock()
		return nil, false
	}
	data := copyMap(snapshot.Facts)
	fs.mu.RUnlock()

	fs.mu.Lock()
	if snapshot, ok = fs.universes[universe]; ok {
		snapshot.LastAccess = time.Now()
	}
	fs.mu.Unlock()

	return data, true
}

func (fs *memoryFactStore) Summaries() []universeSummary {
	if fs == nil {
		return nil
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return buildSummaries(fs.universes)
}

func (fs *memoryFactStore) StoreType() string {
	return "memory"
}

type fileFactStore struct {
	mu        sync.RWMutex
	path      string
	universes map[string]*universeSnapshot
	config    factStoreConfig
}

func newFileFactStore(path string, config factStoreConfig) (*fileFactStore, error) {
	store := &fileFactStore{
		path:      path,
		universes: make(map[string]*universeSnapshot),
		config:    config,
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (fs *fileFactStore) StoreType() string {
	return "file"
}

func (fs *fileFactStore) Update(universe string, facts map[string]interface{}) error {
	if fs == nil {
		return nil
	}
	if universe == "" {
		universe = "default"
	}

	fs.mu.Lock()
	snapshot, ok := fs.universes[universe]
	if !ok {
		snapshot = &universeSnapshot{
			Facts:           make(map[string]interface{}),
			NamespaceAccess: make(map[string]time.Time),
		}
		fs.universes[universe] = snapshot
	}
	nextFacts := copyMap(snapshot.Facts)
	for namespace, payload := range facts {
		existing := nextFacts[namespace]
		strategy := fs.config.strategyFor(namespace)
		merged, _, err := mergeValue(existing, payload, strategy)
		if err != nil {
			fs.mu.Unlock()
			return fmt.Errorf("merge conflict in namespace %q: %w", namespace, err)
		}
		if existing == nil {
			merged = payload
		}
		nextFacts[namespace] = merged
		snapshot.NamespaceAccess[namespace] = time.Now()
	}
	snapshot.Facts = nextFacts
	snapshot.UpdatedAt = time.Now()
	snapshot.LastAccess = time.Now()
	if fs.config.cache.enabled() {
		applyNamespaceLimit(snapshot, fs.config.cache.maxNamespaces)
		applyUniverseLimit(fs.universes, fs.config.cache.maxUniverses)
	}
	persisted := fs.snapshotLocked()
	fs.mu.Unlock()

	return fs.persist(persisted)
}

func (fs *fileFactStore) Snapshot(universe string) (map[string]interface{}, bool) {
	if fs == nil {
		return nil, false
	}
	fs.mu.RLock()
	snapshot, ok := fs.universes[universe]
	if !ok {
		fs.mu.RUnlock()
		return nil, false
	}
	data := copyMap(snapshot.Facts)
	fs.mu.RUnlock()

	fs.mu.Lock()
	if snapshot, ok = fs.universes[universe]; ok {
		snapshot.LastAccess = time.Now()
	}
	fs.mu.Unlock()

	return data, true
}

func (fs *fileFactStore) Summaries() []universeSummary {
	if fs == nil {
		return nil
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return buildSummaries(fs.universes)
}

type persistedFacts struct {
	UpdatedAt time.Time                    `json:"updated_at"`
	Universes map[string]persistedUniverse `json:"universes"`
}

type persistedUniverse struct {
	Facts      map[string]interface{} `json:"facts"`
	UpdatedAt  time.Time              `json:"updated_at"`
	AccessedAt time.Time              `json:"accessed_at,omitempty"`
}

func (fs *fileFactStore) snapshotLocked() persistedFacts {
	persisted := persistedFacts{
		UpdatedAt: time.Now(),
		Universes: make(map[string]persistedUniverse, len(fs.universes)),
	}
	for name, snapshot := range fs.universes {
		persisted.Universes[name] = persistedUniverse{
			Facts:      copyMap(snapshot.Facts),
			UpdatedAt:  snapshot.UpdatedAt,
			AccessedAt: snapshot.LastAccess,
		}
	}
	return persisted
}

func (fs *fileFactStore) persist(data persistedFacts) error {
	if fs == nil || fs.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(fs.path), 0o755); err != nil {
		return err
	}
	tempPath := fs.path + ".tmp"
	file, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, fs.path)
}

func (fs *fileFactStore) load() error {
	if fs == nil || fs.path == "" {
		return nil
	}
	file, err := os.Open(fs.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	payload, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	var data persistedFacts
	if err := json.Unmarshal(payload, &data); err != nil {
		return err
	}
	for name, universe := range data.Universes {
		accessed := universe.AccessedAt
		if accessed.IsZero() {
			accessed = universe.UpdatedAt
		}
		if accessed.IsZero() {
			accessed = time.Now()
		}
		fs.universes[name] = &universeSnapshot{
			Facts:           copyMap(universe.Facts),
			UpdatedAt:       universe.UpdatedAt,
			LastAccess:      accessed,
			NamespaceAccess: make(map[string]time.Time),
		}
	}
	return nil
}

func buildSummaries(universes map[string]*universeSnapshot) []universeSummary {
	summaries := make([]universeSummary, 0, len(universes))
	for name, snapshot := range universes {
		namespaces := make([]string, 0, len(snapshot.Facts))
		for ns := range snapshot.Facts {
			namespaces = append(namespaces, ns)
		}
		sort.Strings(namespaces)
		summaries = append(summaries, universeSummary{
			Universe:   name,
			Namespaces: namespaces,
			UpdatedAt:  snapshot.UpdatedAt,
		})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Universe < summaries[j].Universe })
	return summaries
}

func mergeValue(existing, incoming interface{}, strategy pathutil.MergeStrategy) (interface{}, bool, error) {
	if existing == nil {
		return incoming, incoming != nil, nil
	}
	if incoming == nil {
		return existing, false, nil
	}
	if reflect.DeepEqual(existing, incoming) {
		return existing, false, nil
	}

	existingMap, okExisting := existing.(map[string]interface{})
	incomingMap, okIncoming := incoming.(map[string]interface{})
	if okExisting && okIncoming {
		merged := copyMap(existingMap)
		changed := false
		for key, value := range incomingMap {
			if current, ok := existingMap[key]; ok {
				next, didChange, err := mergeValue(current, value, strategy)
				if err != nil {
					return nil, false, err
				}
				merged[key] = next
				if didChange {
					changed = true
				}
			} else {
				merged[key] = value
				changed = true
			}
		}
		return merged, changed, nil
	}

	switch strategy {
	case pathutil.MergeFirst:
		return existing, false, nil
	case pathutil.MergeLast:
		return incoming, true, nil
	case pathutil.MergeError:
		return nil, false, fmt.Errorf("conflicting values")
	default:
		return incoming, true, nil
	}
}

func applyUniverseLimit(universes map[string]*universeSnapshot, max int) {
	if max <= 0 || len(universes) <= max {
		return
	}
	type candidate struct {
		name string
		at   time.Time
	}
	candidates := make([]candidate, 0, len(universes))
	for name, snapshot := range universes {
		at := snapshot.LastAccess
		if at.IsZero() {
			at = snapshot.UpdatedAt
		}
		candidates = append(candidates, candidate{name: name, at: at})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].at.Before(candidates[j].at) })
	evictCount := len(universes) - max
	for i := 0; i < evictCount; i++ {
		delete(universes, candidates[i].name)
	}
}

func applyNamespaceLimit(snapshot *universeSnapshot, max int) {
	if snapshot == nil || max <= 0 || len(snapshot.Facts) <= max {
		return
	}
	type candidate struct {
		name string
		at   time.Time
	}
	candidates := make([]candidate, 0, len(snapshot.Facts))
	for name := range snapshot.Facts {
		at := snapshot.NamespaceAccess[name]
		if at.IsZero() {
			at = snapshot.UpdatedAt
		}
		candidates = append(candidates, candidate{name: name, at: at})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].at.Before(candidates[j].at) })
	evictCount := len(snapshot.Facts) - max
	for i := 0; i < evictCount; i++ {
		delete(snapshot.Facts, candidates[i].name)
		delete(snapshot.NamespaceAccess, candidates[i].name)
	}
}

func copyMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	copyData := make(map[string]interface{}, len(input))
	for k, v := range input {
		copyData[k] = v
	}
	return copyData
}

func storeTypeOrUnknown(store factStore) string {
	if store == nil {
		return "unknown"
	}
	return store.StoreType()
}

func summariesOrEmpty(store factStore) []universeSummary {
	if store == nil {
		return nil
	}
	return store.Summaries()
}

func factConfigSummary(config factStoreConfig) *factStoreConfigSummary {
	summary := &factStoreConfigSummary{
		DefaultMerge: string(config.defaultStrategy),
		Cache: factCacheSummary{
			Policy:        config.cache.policy,
			MaxUniverses:  config.cache.maxUniverses,
			MaxNamespaces: config.cache.maxNamespaces,
		},
	}
	if len(config.perNamespace) > 0 {
		summary.NamespaceMerge = make(map[string]string, len(config.perNamespace))
		for namespace, strategy := range config.perNamespace {
			summary.NamespaceMerge[namespace] = string(strategy)
		}
	}
	return summary
}

type statusResponse struct {
	Bundle          *bundleSummary          `json:"bundle"`
	Counts          bundleCounts            `json:"counts"`
	StartedAt       time.Time               `json:"started_at"`
	LastReload      time.Time               `json:"last_reload"`
	UptimeSec       int64                   `json:"uptime_sec"`
	Universes       []universeSummary       `json:"universes"`
	RequiredFact    []string                `json:"required_facts"`
	FactStore       string                  `json:"fact_store"`
	FactStoreConfig *factStoreConfigSummary `json:"fact_store_config,omitempty"`
	VerbCount       int                     `json:"verb_count"`
	FactCount       int                     `json:"fact_count"`
}

type bundleSummary struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	VerbHash    string    `json:"verb_hash"`
	CreatedAt   time.Time `json:"created_at"`
	SchemaFiles []string  `json:"schema_files"`
	RuleFiles   []string  `json:"rule_files"`
	VerbFiles   []string  `json:"verb_files"`
}

type bundleCounts struct {
	Rules int `json:"rules"`
	Flows int `json:"flows"`
}

type factStoreConfigSummary struct {
	DefaultMerge   string            `json:"default_merge"`
	NamespaceMerge map[string]string `json:"namespace_merge,omitempty"`
	Cache          factCacheSummary  `json:"cache"`
}

type factCacheSummary struct {
	Policy        string `json:"policy"`
	MaxUniverses  int    `json:"max_universes"`
	MaxNamespaces int    `json:"max_namespaces"`
}

func (s *serverState) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	bundle := s.Bundle()
	if bundle == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "bundle not loaded")
		return
	}
	resp := statusResponse{
		Bundle: &bundleSummary{
			Name:        bundle.Name,
			Version:     bundle.Version,
			Description: bundle.Description,
			VerbHash:    bundle.VerbHash,
			CreatedAt:   bundle.CreatedAt,
			SchemaFiles: bundle.SchemaFiles,
			RuleFiles:   bundle.RuleFiles,
			VerbFiles:   bundle.VerbFiles,
		},
		Counts: bundleCounts{
			Rules: countRules(bundle),
			Flows: countFlows(bundle),
		},
		StartedAt:       s.startedAt,
		LastReload:      s.updatedAt,
		UptimeSec:       int64(time.Since(s.startedAt).Seconds()),
		Universes:       summariesOrEmpty(s.factStore),
		RequiredFact:    bundle.RequiredFacts,
		FactStore:       storeTypeOrUnknown(s.factStore),
		FactStoreConfig: factConfigSummary(s.factConfig),
		VerbCount:       len(bundle.VerbSpecs),
		FactCount:       len(bundle.FactTypes),
	}
	writeJSON(w, http.StatusOK, resp)
}

type effectInfo struct {
	Verb string                 `json:"verb"`
	Args map[string]interface{} `json:"args"`
}

func (s *serverState) handleRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	bundle := s.Bundle()
	if bundle == nil {
		writeJSON(w, http.StatusOK, []unified.RuleSummary{})
		return
	}
	rules := bundle.Rules
	if len(rules) == 0 && bundle.ListSpec != nil {
		rules = unified.SummarizeRules(bundle.ListSpec)
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *serverState) handleRuleSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	bundle := s.Bundle()
	if bundle == nil {
		writeJSON(w, http.StatusOK, []unified.RuleSource{})
		return
	}
	if path := strings.TrimSpace(r.URL.Query().Get("path")); path != "" {
		for _, source := range bundle.RuleSources {
			if source.Path == path {
				writeJSON(w, http.StatusOK, source)
				return
			}
		}
		writeJSONError(w, http.StatusNotFound, "rule source not found")
		return
	}
	writeJSON(w, http.StatusOK, bundle.RuleSources)
}

func (s *serverState) handleFlows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	bundle := s.Bundle()
	if bundle == nil {
		writeJSON(w, http.StatusOK, []unified.FlowSummary{})
		return
	}
	flows := bundle.Flows
	if len(flows) == 0 && bundle.FlowSpec != nil {
		flows = unified.SummarizeFlows(bundle.FlowSpec)
	}
	writeJSON(w, http.StatusOK, flows)
}

type graphNode struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

type graphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type graphResponse struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

func (s *serverState) handleGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	bundle := s.Bundle()
	if bundle == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "bundle not loaded")
		return
	}
	nodeMap := make(map[string]graphNode)
	edges := make([]graphEdge, 0)

	addNode := func(id, kind, label string) {
		if _, ok := nodeMap[id]; ok {
			return
		}
		nodeMap[id] = graphNode{ID: id, Kind: kind, Label: label}
	}

	rules := bundle.Rules
	if len(rules) == 0 && bundle.ListSpec != nil {
		rules = unified.SummarizeRules(bundle.ListSpec)
	}
	for _, rule := range rules {
		ruleID := "rule:" + rule.Name
		addNode(ruleID, "rule", rule.Name)
		for _, fact := range rule.FactPaths {
			factID := "fact:" + fact
			addNode(factID, "fact", fact)
			edges = append(edges, graphEdge{From: factID, To: ruleID, Kind: "uses"})
		}
		for _, effect := range rule.Effects {
			verbID := "verb:" + effect.Verb
			addNode(verbID, "verb", effect.Verb)
			edges = append(edges, graphEdge{From: ruleID, To: verbID, Kind: "emits"})
		}
	}

	flows := bundle.Flows
	if len(flows) == 0 && bundle.FlowSpec != nil {
		flows = unified.SummarizeFlows(bundle.FlowSpec)
	}
	for _, flowSpec := range flows {
		flowID := "flow:" + flowSpec.Name
		addNode(flowID, "flow", flowSpec.Name)
		for _, fact := range flowSpec.FactPaths {
			factID := "fact:" + fact
			addNode(factID, "fact", fact)
			edges = append(edges, graphEdge{From: factID, To: flowID, Kind: "uses"})
		}
		for _, verb := range flowSpec.Verbs {
			verbID := "verb:" + verb
			addNode(verbID, "verb", verb)
			edges = append(edges, graphEdge{From: flowID, To: verbID, Kind: "emits"})
		}
	}

	nodes := make([]graphNode, 0, len(nodeMap))
	for _, node := range nodeMap {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	writeJSON(w, http.StatusOK, graphResponse{Nodes: nodes, Edges: edges})
}

func (s *serverState) handleVerbs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	bundle := s.Bundle()
	if bundle == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "bundle not loaded")
		return
	}
	if len(bundle.VerbSpecs) == 0 {
		writeJSON(w, http.StatusOK, []unified.VerbSpecSummary{})
		return
	}
	writeJSON(w, http.StatusOK, bundle.VerbSpecs)
}

func (s *serverState) handleSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	bundle := s.Bundle()
	if bundle == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "bundle not loaded")
		return
	}
	if len(bundle.FactTypes) == 0 {
		writeJSON(w, http.StatusOK, []unified.FactTypeSummary{})
		return
	}
	writeJSON(w, http.StatusOK, bundle.FactTypes)
}

func (s *serverState) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *serverState) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	bundle := s.Bundle()
	if bundle == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "bundle not loaded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ready",
		"bundle":  bundle.Name,
		"version": bundle.Version,
	})
}

type factIngestRequest struct {
	Universe string                 `json:"universe"`
	Facts    map[string]interface{} `json:"facts"`
}

func (s *serverState) handleFacts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req factIngestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if req.Facts == nil {
			writeJSONError(w, http.StatusBadRequest, "facts are required")
			return
		}
		if err := s.IngestFacts(factEnvelope{Universe: req.Universe, Facts: req.Facts}); err != nil {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	case http.MethodGet:
		universe := r.URL.Query().Get("universe")
		if universe == "" {
			writeJSONError(w, http.StatusBadRequest, "universe is required")
			return
		}
		if s.factStore == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "fact store not configured")
			return
		}
		snapshot, ok := s.factStore.Snapshot(universe)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "universe not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"universe": universe,
			"facts":    snapshot,
		})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type dryRunRequest struct {
	Universe  string                 `json:"universe"`
	Facts     map[string]interface{} `json:"facts"`
	Mode      string                 `json:"mode"`
	UseStored bool                   `json:"use_stored"`
}

type dryRunResponse struct {
	Universe string         `json:"universe"`
	Mode     string         `json:"mode"`
	Rules    []dryRunRule   `json:"rules,omitempty"`
	Flows    []dryRunFlow   `json:"flows,omitempty"`
	Summary  dryRunSummary  `json:"summary"`
	Errors   []string       `json:"errors,omitempty"`
	Facts    map[string]int `json:"facts,omitempty"`
}

type dryRunSummary struct {
	RulesMatched int `json:"rules_matched"`
	RulesTotal   int `json:"rules_total"`
	FlowsMatched int `json:"flows_matched"`
	FlowsTotal   int `json:"flows_total"`
}

type dryRunRule struct {
	Name       string          `json:"name"`
	Priority   int             `json:"priority"`
	Matched    bool            `json:"matched"`
	Predicates []predicateEval `json:"predicates"`
	Effects    []effectInfo    `json:"effects"`
}

type dryRunFlow struct {
	Name       string          `json:"name"`
	Priority   int             `json:"priority"`
	Matched    bool            `json:"matched"`
	Predicates []predicateEval `json:"predicates"`
	Verbs      []string        `json:"verbs"`
}

type predicateEval struct {
	Expression string `json:"expression"`
	Value      bool   `json:"value"`
	Error      string `json:"error,omitempty"`
}

func (s *serverState) handleDryRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req dryRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Universe == "" {
		req.Universe = "default"
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "list"
	}
	facts := req.Facts
	if len(facts) == 0 && req.UseStored {
		if s.factStore != nil {
			if snapshot, ok := s.factStore.Snapshot(req.Universe); ok {
				facts = snapshot
			}
		}
	}
	if len(facts) == 0 {
		writeJSONError(w, http.StatusBadRequest, "facts are required")
		return
	}

	bundle := s.Bundle()
	if bundle == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "bundle not loaded")
		return
	}

	registry := schema.NewRegistry()
	registry.LoadFromMap(facts)

	resp := dryRunResponse{
		Universe: req.Universe,
		Mode:     mode,
		Facts:    map[string]int{"namespaces": len(facts)},
	}

	if mode == "list" || mode == "both" {
		rules := bundle.Rules
		if len(rules) == 0 && bundle.ListSpec != nil {
			rules = unified.SummarizeRules(bundle.ListSpec)
		}
		evaluated, matched := evaluateRules(rules, registry)
		resp.Rules = evaluated
		resp.Summary.RulesMatched = matched
		resp.Summary.RulesTotal = len(evaluated)
	}
	if mode == "flow" || mode == "both" {
		flows := bundle.Flows
		if len(flows) == 0 && bundle.FlowSpec != nil {
			flows = unified.SummarizeFlows(bundle.FlowSpec)
		}
		evaluated, matched := evaluateFlows(flows, registry)
		resp.Flows = evaluated
		resp.Summary.FlowsMatched = matched
		resp.Summary.FlowsTotal = len(evaluated)
	}

	writeJSON(w, http.StatusOK, resp)
}

func evaluateRules(rules []unified.RuleSummary, registry *schema.Registry) ([]dryRunRule, int) {
	if len(rules) == 0 {
		return nil, 0
	}
	evaluated := make([]dryRunRule, 0, len(rules))
	matched := 0
	for _, rule := range rules {
		predicates, ok := evaluatePredicates(registry, rule.Predicates)
		effects := make([]effectInfo, 0, len(rule.Effects))
		for _, effect := range rule.Effects {
			effects = append(effects, effectInfo{Verb: effect.Verb, Args: effect.Args})
		}
		evaluated = append(evaluated, dryRunRule{
			Name:       rule.Name,
			Priority:   rule.Priority,
			Matched:    ok,
			Predicates: predicates,
			Effects:    effects,
		})
		if ok {
			matched++
		}
	}
	return evaluated, matched
}

func evaluateFlows(flows []unified.FlowSummary, registry *schema.Registry) ([]dryRunFlow, int) {
	if len(flows) == 0 {
		return nil, 0
	}
	evaluated := make([]dryRunFlow, 0, len(flows))
	matched := 0
	for _, flowSpec := range flows {
		predicates, ok := evaluatePredicates(registry, flowSpec.Predicates)
		evaluated = append(evaluated, dryRunFlow{
			Name:       flowSpec.Name,
			Priority:   flowSpec.Priority,
			Matched:    ok,
			Predicates: predicates,
			Verbs:      append([]string(nil), flowSpec.Verbs...),
		})
		if ok {
			matched++
		}
	}
	return evaluated, matched
}

func evaluatePredicates(registry *schema.Registry, predicates []string) ([]predicateEval, bool) {
	if len(predicates) == 0 {
		return nil, true
	}
	results := make([]predicateEval, 0, len(predicates))
	matched := true
	for _, expression := range predicates {
		value, err := registry.EvaluateBoolean(expression)
		entry := predicateEval{Expression: expression, Value: value}
		if err != nil {
			entry.Error = err.Error()
			matched = false
		}
		if !value {
			matched = false
		}
		results = append(results, entry)
	}
	return results, matched
}

func countRules(bundle *unified.Bundle) int {
	if bundle == nil {
		return 0
	}
	if len(bundle.Rules) > 0 {
		return len(bundle.Rules)
	}
	if bundle.ListSpec != nil {
		return len(bundle.ListSpec.Rules)
	}
	return 0
}

func countFlows(bundle *unified.Bundle) int {
	if bundle == nil {
		return 0
	}
	if len(bundle.Flows) > 0 {
		return len(bundle.Flows)
	}
	if bundle.FlowSpec != nil {
		return len(bundle.FlowSpec.Flows)
	}
	return 0
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *serverState) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/ui" {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(uiHTML))
}

const uiHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Effectus Status</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f3ef;
      --bg-2: #e8efe9;
      --card: #ffffff;
      --ink: #1c1a18;
      --muted: #5f5a54;
      --accent: #0d6b6b;
      --accent-2: #d97706;
      --border: #e4ddd5;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Space Grotesk", "Avenir Next", "Segoe UI", sans-serif;
      color: var(--ink);
      background: radial-gradient(circle at top left, var(--bg-2), var(--bg) 55%);
    }
    header {
      padding: 28px 24px 12px;
    }
    header h1 {
      margin: 0 0 6px;
      font-size: 28px;
      letter-spacing: 0.4px;
    }
    header p {
      margin: 0;
      color: var(--muted);
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
      gap: 16px;
      padding: 16px 24px 32px;
    }
    .card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 14px;
      padding: 16px;
      box-shadow: 0 10px 24px rgba(28, 26, 24, 0.08);
      animation: fadeIn 0.5s ease;
    }
    .card h2 {
      margin: 0 0 10px;
      font-size: 16px;
      text-transform: uppercase;
      letter-spacing: 1px;
      color: var(--accent);
    }
    .card pre {
      background: #f9f7f3;
      border-radius: 10px;
      padding: 12px;
      font-size: 12px;
      overflow-x: auto;
      border: 1px solid #eee7df;
    }
    .muted {
      color: var(--muted);
      font-size: 12px;
    }
    .list {
      display: grid;
      gap: 8px;
    }
    .sources-grid {
      display: grid;
      gap: 16px;
      grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
      align-items: start;
    }
    .list-item {
      padding: 10px;
      border-radius: 10px;
      border: 1px solid var(--border);
      background: #fffdfa;
    }
    .list-item.matched {
      border-color: rgba(13, 107, 107, 0.4);
      background: rgba(13, 107, 107, 0.08);
    }
    .list-item.unmatched {
      border-color: rgba(217, 119, 6, 0.4);
      background: rgba(217, 119, 6, 0.08);
    }
    .row {
      display: flex;
      gap: 12px;
      flex-wrap: wrap;
      align-items: center;
    }
    .pill {
      background: #f2ece4;
      padding: 4px 10px;
      border-radius: 999px;
      font-size: 12px;
      color: var(--muted);
    }
    .pill.good { color: #0d6b6b; background: rgba(13, 107, 107, 0.12); }
    .pill.bad { color: #b45309; background: rgba(217, 119, 6, 0.15); }
    .graph {
      width: 100%;
      height: 280px;
      border: 1px solid var(--border);
      border-radius: 12px;
      background: #fffdf9;
    }
    button {
      border: none;
      padding: 10px 16px;
      border-radius: 10px;
      background: var(--accent);
      color: white;
      cursor: pointer;
      font-weight: 600;
    }
    button.secondary {
      background: var(--accent-2);
    }
    textarea, input, select {
      width: 100%;
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 10px;
      font-family: "JetBrains Mono", "SF Mono", "Fira Code", monospace;
      font-size: 12px;
      background: #fffdfb;
    }
    label {
      font-size: 12px;
      color: var(--muted);
      display: block;
      margin-bottom: 6px;
    }
    .stack { display: grid; gap: 10px; }
    @keyframes fadeIn {
      from { opacity: 0; transform: translateY(6px); }
      to { opacity: 1; transform: translateY(0); }
    }
  </style>
</head>
<body>
  <header>
    <h1>Effectus Status</h1>
    <p>Live bundle view, rule graph, and playground for dry runs.</p>
  </header>
  <section class="grid">
    <div class="card">
      <div class="row" style="justify-content: space-between;">
        <h2>Overview</h2>
        <button onclick="refreshAll()">Refresh</button>
      </div>
      <div class="stack">
        <div class="row">
          <div style="flex: 1;">
            <label>API Token</label>
            <input id="api-token" placeholder="paste token for /api access" />
          </div>
          <button onclick="saveToken()">Save</button>
        </div>
        <div id="status" class="stack"></div>
      </div>
    </div>
    <div class="card">
      <h2>Rules</h2>
      <pre id="rules">Loading...</pre>
    </div>
    <div class="card" style="grid-column: 1 / -1;">
      <h2>Sources</h2>
      <div class="sources-grid">
        <div>
          <div class="muted">Rules (.eff)</div>
          <div id="rule-sources" class="list"></div>
        </div>
        <div>
          <div class="muted">Flows (.effx)</div>
          <div id="flow-sources" class="list"></div>
        </div>
      </div>
    </div>
    <div class="card">
      <h2>Flows</h2>
      <pre id="flows">Loading...</pre>
    </div>
    <div class="card">
      <h2>Verb Specs</h2>
      <pre id="verbs">Loading...</pre>
    </div>
    <div class="card">
      <h2>Schema</h2>
      <pre id="schema">Loading...</pre>
    </div>
    <div class="card">
      <h2>Graph</h2>
      <svg id="graph-svg" class="graph" viewBox="0 0 800 280" preserveAspectRatio="xMidYMid meet"></svg>
      <details>
        <summary class="muted">Raw graph JSON</summary>
        <pre id="graph">Loading...</pre>
      </details>
    </div>
    <div class="card" style="grid-column: 1 / -1;">
      <h2>Facts Browser</h2>
      <div class="stack">
        <div id="facts-merge-config" class="muted"></div>
        <div class="row">
          <div style="flex: 1;">
            <label>Universe</label>
            <select id="facts-universe"></select>
          </div>
          <button onclick="loadUniverseFacts()">Load</button>
        </div>
        <div id="facts-browser" class="list"></div>
      </div>
    </div>
    <div class="card" style="grid-column: 1 / -1;">
      <h2>Playground</h2>
      <div class="stack">
        <div>
          <label>Universe (optional)</label>
          <input id="universe" placeholder="default" />
        </div>
        <div>
          <label>Mode</label>
          <select id="mode">
            <option value="list">List rules</option>
            <option value="flow">Flow rules</option>
            <option value="both">Both</option>
          </select>
        </div>
        <div class="row">
          <label class="muted"><input type="checkbox" id="use-stored" checked /> Use stored facts when empty</label>
          <button onclick="loadStoredFacts()">Load stored</button>
        </div>
        <div>
          <label>Facts JSON (namespaced at top level)</label>
          <textarea id="facts" rows="10" placeholder='{"order": {"total": 120}, "customer": {"tier": "gold"}}'></textarea>
        </div>
        <div class="row">
          <button class="secondary" onclick="runDryRun()">Dry Run</button>
          <button onclick="ingestFacts()">Ingest Facts</button>
          <span class="pill" id="dry-run-status">Ready</span>
        </div>
        <div id="dry-run-summary" class="row"></div>
        <div id="dry-run-results" class="list"></div>
        <details>
          <summary class="muted">Raw JSON</summary>
          <pre id="dry-run-raw">Dry run output appears here.</pre>
        </details>
      </div>
    </div>
  </section>
  <script>
    const TOKEN_KEY = "effectus_api_token";

    const render = (id, data) => {
      const el = document.getElementById(id);
      if (!el) return;
      el.textContent = JSON.stringify(data, null, 2);
    };

    const escapeHtml = (value) => {
      return String(value)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;");
    };

    const getToken = () => {
      const input = document.getElementById("api-token");
      if (input && input.value.trim()) {
        return input.value.trim();
      }
      return localStorage.getItem(TOKEN_KEY) || "";
    };

    const authHeaders = () => {
      const token = getToken();
      if (!token) return {};
      return { "Authorization": "Bearer " + token };
    };

    const formatMergeConfig = (config) => {
      if (!config) return null;
      const defaultMerge = config.default_merge || "-";
      const nsConfig = config.namespace_merge || {};
      const nsEntries = Object.keys(nsConfig)
        .sort()
        .map(key => key + "=" + nsConfig[key]);
      const nsText = nsEntries.length ? nsEntries.join(", ") : "none";
      const cache = config.cache || {};
      const policy = cache.policy || "none";
      let cacheText = policy;
      if (policy !== "none") {
        const maxU = cache.max_universes || 0;
        const maxN = cache.max_namespaces || 0;
        const maxUText = maxU > 0 ? maxU : "unlimited";
        const maxNText = maxN > 0 ? maxN : "unlimited";
        cacheText += " (universes " + maxUText + ", namespaces " + maxNText + ")";
      }
      return {
        merge: "default " + defaultMerge + ", namespaces " + nsText,
        cache: cacheText
      };
    };

    const renderFactsMergeConfig = (status) => {
      const target = document.getElementById("facts-merge-config");
      if (!target) return;
      const config = formatMergeConfig(status ? status.fact_store_config : null);
      if (!config) {
        target.textContent = "";
        return;
      }
      target.textContent = "Merge: " + config.merge + " · Cache: " + config.cache;
    };

    function saveToken() {
      const input = document.getElementById("api-token");
      if (!input) return;
      const value = input.value.trim();
      if (value) {
        localStorage.setItem(TOKEN_KEY, value);
      } else {
        localStorage.removeItem(TOKEN_KEY);
      }
      refreshAll();
    }

    const renderStatus = (status) => {
      const el = document.getElementById("status");
      if (!el) return;
      const bundle = status.bundle || {};
      const mergeConfig = formatMergeConfig(status.fact_store_config);
      const mergeHTML = mergeConfig
        ? '<div class="muted">merge: ' + escapeHtml(mergeConfig.merge) + '</div>' +
          '<div class="muted">cache: ' + escapeHtml(mergeConfig.cache) + '</div>'
        : "";
      el.innerHTML =
        '<div class="row">' +
          '<span class="pill">' + escapeHtml(bundle.name || '-') + '</span>' +
          '<span class="pill">v' + escapeHtml(bundle.version || '-') + '</span>' +
          '<span class="pill">rules: ' + (status.counts ? status.counts.rules : 0) + '</span>' +
          '<span class="pill">flows: ' + (status.counts ? status.counts.flows : 0) + '</span>' +
          '<span class="pill">verbs: ' + (status.verb_count || 0) + '</span>' +
          '<span class="pill">facts: ' + (status.fact_count || 0) + '</span>' +
          '<span class="pill">store: ' + (status.fact_store || '-') + '</span>' +
        '</div>' +
        '<div class="row">' +
          '<span class="pill">uptime: ' + status.uptime_sec + 's</span>' +
          '<span class="pill">universes: ' + (status.universes ? status.universes.length : 0) + '</span>' +
        '</div>' +
        mergeHTML +
        '<div style="color: var(--muted); font-size: 12px;">' + escapeHtml(bundle.description || '') + '</div>';
    };

    const renderDryRun = (data) => {
      const summary = document.getElementById("dry-run-summary");
      const results = document.getElementById("dry-run-results");
      const raw = document.getElementById("dry-run-raw");
      if (raw) {
        raw.textContent = JSON.stringify(data, null, 2);
      }
      if (!summary || !results) return;
      if (!data || data.error) {
        summary.innerHTML = '<span class="pill bad">Error</span>';
        results.innerHTML = '';
        return;
      }
      const summaryHTML =
        '<span class="pill good">rules: ' + (data.summary ? data.summary.rules_matched : 0) + '/' + (data.summary ? data.summary.rules_total : 0) + '</span>' +
        '<span class="pill good">flows: ' + (data.summary ? data.summary.flows_matched : 0) + '/' + (data.summary ? data.summary.flows_total : 0) + '</span>';
      summary.innerHTML = summaryHTML;

      let html = '';
      if (data.rules && data.rules.length) {
        html += '<div class="muted">Rules</div>';
        data.rules.forEach(rule => {
          const klass = rule.matched ? 'matched' : 'unmatched';
          html += '<div class="list-item ' + klass + '">' +
            '<div class="row">' +
              '<strong>' + escapeHtml(rule.name || '') + '</strong>' +
              '<span class="pill ' + (rule.matched ? 'good' : 'bad') + '">' + (rule.matched ? 'matched' : 'no match') + '</span>' +
            '</div>' +
            '<div class="muted">predicates: ' + (rule.predicates ? rule.predicates.length : 0) + ', effects: ' + (rule.effects ? rule.effects.length : 0) + '</div>' +
          '</div>';
        });
      }
      if (data.flows && data.flows.length) {
        html += '<div class="muted">Flows</div>';
        data.flows.forEach(flow => {
          const klass = flow.matched ? 'matched' : 'unmatched';
          html += '<div class="list-item ' + klass + '">' +
            '<div class="row">' +
              '<strong>' + escapeHtml(flow.name || '') + '</strong>' +
              '<span class="pill ' + (flow.matched ? 'good' : 'bad') + '">' + (flow.matched ? 'matched' : 'no match') + '</span>' +
            '</div>' +
            '<div class="muted">predicates: ' + (flow.predicates ? flow.predicates.length : 0) + ', verbs: ' + (flow.verbs ? flow.verbs.length : 0) + '</div>' +
          '</div>';
        });
      }
      if (!html) {
        html = '<div class="muted">No rules or flows returned.</div>';
      }
      results.innerHTML = html;
    };

    const populateUniverseOptions = (status) => {
      const select = document.getElementById("facts-universe");
      if (!select) return;
      const current = select.value || document.getElementById("universe").value || "";
      const options = [];
      if (status && status.universes) {
        status.universes.forEach(entry => options.push(entry.universe));
      }
      if (options.indexOf("default") === -1) {
        options.unshift("default");
      }
      const unique = Array.from(new Set(options));
      select.innerHTML = "";
      unique.forEach(name => {
        const opt = document.createElement("option");
        opt.value = name;
        opt.textContent = name;
        select.appendChild(opt);
      });
      if (current) {
        select.value = current;
      }
    };

    const renderFactsBrowser = (universe, facts) => {
      const container = document.getElementById("facts-browser");
      if (!container) return;
      if (!facts) {
        container.innerHTML = '<div class="muted">No facts loaded.</div>';
        return;
      }
      const namespaces = Object.keys(facts);
      namespaces.sort();
      if (namespaces.length === 0) {
        container.innerHTML = '<div class="muted">No namespaces for ' + escapeHtml(universe) + '.</div>';
        return;
      }
      let html = '';
      namespaces.forEach(ns => {
        const payload = facts[ns];
        html += '<details class="list-item">' +
          '<summary><strong>' + escapeHtml(ns) + '</strong></summary>' +
          '<pre>' + escapeHtml(JSON.stringify(payload, null, 2)) + '</pre>' +
        '</details>';
      });
      container.innerHTML = html;
    };

    const renderRuleSources = (sources) => {
      const ruleContainer = document.getElementById("rule-sources");
      const flowContainer = document.getElementById("flow-sources");
      if (!ruleContainer || !flowContainer) return;

      if (!sources || sources.length === 0) {
        ruleContainer.innerHTML = '<div class="muted">No rule sources stored in bundle.</div>';
        flowContainer.innerHTML = '<div class="muted">No flow sources stored in bundle.</div>';
        return;
      }

      const normalized = sources.map(source => ({
        ...source,
        format: (source.format || '').toLowerCase(),
      }));
      const rules = normalized.filter(source => source.format !== 'effx');
      const flows = normalized.filter(source => source.format === 'effx');

      const renderList = (container, items, emptyMsg) => {
        if (!container) return;
        if (!items || items.length === 0) {
          container.innerHTML = '<div class="muted">' + emptyMsg + '</div>';
          return;
        }
        const sorted = items.slice().sort((a, b) => (a.path || '').localeCompare(b.path || ''));
        let html = '';
        sorted.forEach(source => {
          const path = source.path || 'rule';
          const format = source.format || '';
          html += '<details class="list-item">' +
            '<summary class="row">' +
              '<strong>' + escapeHtml(path) + '</strong>' +
              (format ? '<span class="pill">' + escapeHtml(format) + '</span>' : '') +
            '</summary>' +
            '<pre>' + escapeHtml(source.content || '') + '</pre>' +
          '</details>';
        });
        container.innerHTML = html;
      };

      renderList(ruleContainer, rules, 'No rule sources stored in bundle.');
      renderList(flowContainer, flows, 'No flow sources stored in bundle.');
    };

    const renderGraphSvg = (graph) => {
      const svg = document.getElementById("graph-svg");
      if (!svg || !graph || !graph.nodes) {
        return;
      }
      const width = svg.clientWidth || 800;
      const columnX = [40, Math.max(240, width / 2 - 80), Math.max(420, width - 220)];
      const columns = [[], [], []];
      const columnIndex = { fact: 0, rule: 1, flow: 1, verb: 2 };

      graph.nodes.forEach(node => {
        const idx = columnIndex[node.kind] !== undefined ? columnIndex[node.kind] : 1;
        columns[idx].push(node);
      });
      columns.forEach(col => col.sort((a, b) => (a.label || '').localeCompare(b.label || '')));

      const rowHeight = 36;
      const maxRows = Math.max(columns[0].length, columns[1].length, columns[2].length, 1);
      const height = Math.max(220, (maxRows + 1) * rowHeight);
      svg.setAttribute("viewBox", "0 0 " + width + " " + height);

      const positions = {};
      columns.forEach((col, colIndex) => {
        const x = columnX[colIndex] || (colIndex + 1) * 200;
        col.forEach((node, idx) => {
          positions[node.id] = { x: x, y: 40 + idx * rowHeight };
        });
      });

      let markup = '';
      const edges = graph.edges || [];
      edges.forEach(edge => {
        const from = positions[edge.from];
        const to = positions[edge.to];
        if (!from || !to) return;
        markup += '<line x1="' + (from.x + 120) + '" y1="' + from.y + '" x2="' + (to.x - 10) + '" y2="' + to.y + '" stroke="#d4cbbf" stroke-width="1.5" />';
      });

      graph.nodes.forEach(node => {
        const pos = positions[node.id];
        if (!pos) return;
        let fill = '#f2ece4';
        let stroke = '#d4cbbf';
        if (node.kind === 'fact') { fill = 'rgba(13,107,107,0.12)'; stroke = '#0d6b6b'; }
        if (node.kind === 'verb') { fill = 'rgba(217,119,6,0.12)'; stroke = '#d97706'; }
        markup += '<rect x="' + pos.x + '" y="' + (pos.y - 14) + '" width="120" height="28" rx="8" ry="8" fill="' + fill + '" stroke="' + stroke + '" />';
        markup += '<text x="' + (pos.x + 8) + '" y="' + (pos.y + 4) + '" font-size="11" fill="#1c1a18">' + escapeHtml(node.label || node.id) + '</text>';
      });

      svg.innerHTML = markup;
    };

    async function fetchJSON(path, options) {
      const opts = options || {};
      opts.headers = Object.assign({}, opts.headers || {}, authHeaders());
      const res = await fetch(path, opts);
      if (!res.ok) {
        throw new Error(path + ' ' + res.status);
      }
      return res.json();
    }

    async function refreshAll() {
      try {
        const status = await fetchJSON("/api/status");
        renderStatus(status);
        populateUniverseOptions(status);
        renderFactsMergeConfig(status);
      } catch (err) {
        render("status", { error: err.message });
      }
      try {
        render("rules", await fetchJSON("/api/rules"));
      } catch (err) {
        render("rules", { error: err.message });
      }
      try {
        const sources = await fetchJSON("/api/rules/source");
        renderRuleSources(sources);
      } catch (err) {
        const ruleContainer = document.getElementById("rule-sources");
        const flowContainer = document.getElementById("flow-sources");
        if (ruleContainer) {
          ruleContainer.innerHTML = '<div class="muted">Error loading rule sources.</div>';
        }
        if (flowContainer) {
          flowContainer.innerHTML = '<div class="muted">Error loading flow sources.</div>';
        }
      }
      try {
        render("flows", await fetchJSON("/api/flows"));
      } catch (err) {
        render("flows", { error: err.message });
      }
      try {
        render("verbs", await fetchJSON("/api/verbs"));
      } catch (err) {
        render("verbs", { error: err.message });
      }
      try {
        render("schema", await fetchJSON("/api/schema"));
      } catch (err) {
        render("schema", { error: err.message });
      }
      try {
        const graph = await fetchJSON("/api/graph");
        render("graph", graph);
        renderGraphSvg(graph);
      } catch (err) {
        render("graph", { error: err.message });
      }
    }

    async function loadUniverseFacts() {
      const select = document.getElementById("facts-universe");
      const universe = select ? select.value : "";
      const statusEl = document.getElementById("dry-run-status");
      if (!universe) {
        statusEl.textContent = "Universe required";
        return;
      }
      statusEl.textContent = "Loading facts...";
      try {
        const data = await fetchJSON("/api/facts?universe=" + encodeURIComponent(universe));
        renderFactsBrowser(universe, data.facts || {});
        document.getElementById("universe").value = universe;
        statusEl.textContent = "Loaded";
      } catch (err) {
        statusEl.textContent = "Error";
      }
    }

    async function ingestFacts() {
      const factsText = document.getElementById("facts").value.trim();
      const universe = document.getElementById("universe").value.trim();
      const statusEl = document.getElementById("dry-run-status");
      if (!factsText) {
        statusEl.textContent = "Add facts JSON first";
        return;
      }
      let facts = null;
      try {
        facts = JSON.parse(factsText);
      } catch (err) {
        statusEl.textContent = "Invalid JSON";
        return;
      }
      statusEl.textContent = "Ingesting...";
      try {
        await fetchJSON("/api/facts", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ universe: universe, facts: facts })
        });
        const select = document.getElementById("facts-universe");
        if (select && universe) {
          select.value = universe;
        }
        statusEl.textContent = "Stored";
        refreshAll();
      } catch (err) {
        statusEl.textContent = "Error";
      }
    }

    async function loadStoredFacts() {
      const universe = document.getElementById("universe").value.trim();
      const statusEl = document.getElementById("dry-run-status");
      if (!universe) {
        statusEl.textContent = "Universe required";
        return;
      }
      statusEl.textContent = "Loading...";
      try {
        const data = await fetchJSON("/api/facts?universe=" + encodeURIComponent(universe));
        document.getElementById("facts").value = JSON.stringify(data.facts || {}, null, 2);
        renderFactsBrowser(universe, data.facts || {});
        statusEl.textContent = "Loaded";
      } catch (err) {
        statusEl.textContent = "Error";
      }
    }

    async function runDryRun() {
      const factsText = document.getElementById("facts").value.trim();
      const universe = document.getElementById("universe").value.trim();
      const mode = document.getElementById("mode").value;
      const useStored = document.getElementById("use-stored").checked;
      let facts = null;
      if (factsText) {
        try {
          facts = JSON.parse(factsText);
        } catch (err) {
          renderDryRun({ error: "invalid JSON", details: err.message });
          return;
        }
      }
      const payload = { universe, mode, facts, use_stored: useStored || !factsText };
      const statusEl = document.getElementById("dry-run-status");
      statusEl.textContent = "Running...";
      try {
        const res = await fetch("/api/playground/dry-run", {
          method: "POST",
          headers: Object.assign({ "Content-Type": "application/json" }, authHeaders()),
          body: JSON.stringify(payload)
        });
        const body = await res.json();
        renderDryRun(body);
        statusEl.textContent = res.ok ? "Done" : "Error";
      } catch (err) {
        renderDryRun({ error: err.message });
        statusEl.textContent = "Error";
      }
    }

    const tokenInput = document.getElementById("api-token");
    if (tokenInput) {
      tokenInput.value = localStorage.getItem(TOKEN_KEY) || "";
    }
    refreshAll();
  </script>
</body>
</html>`
