package schema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/effectus/effectus-go/invocation"
	"github.com/go-redis/redis/v8"
)

const (
	redisOutboxStateVersion = 1
	redisNormalizedMarker   = "normalized-saga-v1"
	defaultRedisSagaBytes   = 4 << 20
)

type RedisOutboxStoreOptions struct {
	Addr           string
	Password       string
	DB             int
	Prefix         string
	TTL            time.Duration
	MaxRetries     int
	MaxSagaBytes   int
	MaxLegacyBytes int
}

// RedisOutboxStore keeps one bounded document per saga. Documents, the ready
// index, and dispatch lookup keys share one Redis Cluster hash tag. Every
// mutation is a Lua compare-and-swap and never rewrites unrelated sagas.
type RedisOutboxStore struct {
	client                               *redis.Client
	base, markerKey, readyKey, legacyKey string
	ttl                                  time.Duration
	maxRetries, maxSagaBytes             int
	conflicts                            atomic.Uint64
}

type redisOutboxState struct {
	Version    int                             `json:"version"`
	Sagas      map[string]*SagaInstance        `json:"sagas"`
	Steps      map[string]map[string]*SagaStep `json:"steps"`
	Dispatches map[string]*Dispatch            `json:"dispatches"`
	Attempts   map[string][]DispatchAttempt    `json:"attempts"`
}

var redisSagaCASScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if ARGV[1] == '' then
  if current then return 0 end
else
  if not current or current ~= ARGV[1] then return 0 end
end
redis.call('SET', KEYS[1], ARGV[2])
if ARGV[3] == '' then redis.call('ZREM', KEYS[2], ARGV[4])
else redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4]) end
for i = 3, #KEYS do
  redis.call('SET', KEYS[i], ARGV[4])
end
local ttl = tonumber(ARGV[5])
if ttl > 0 then
  redis.call('PEXPIRE', KEYS[1], ttl)
  for i = 3, #KEYS do redis.call('PEXPIRE', KEYS[i], ttl) end
else
  redis.call('PERSIST', KEYS[1])
  for i = 3, #KEYS do redis.call('PERSIST', KEYS[i]) end
end
return 1
`)

func NewRedisOutboxStore(options RedisOutboxStoreOptions) (*RedisOutboxStore, error) {
	if strings.TrimSpace(options.Addr) == "" {
		return nil, fmt.Errorf("Redis outbox address is required")
	}
	if options.TTL < 0 {
		return nil, fmt.Errorf("Redis outbox TTL must not be negative")
	}
	if options.MaxRetries <= 0 {
		options.MaxRetries = 64
	}
	if options.MaxSagaBytes <= 0 {
		options.MaxSagaBytes = defaultRedisSagaBytes
	}
	client := redis.NewClient(&redis.Options{Addr: options.Addr, Password: options.Password, DB: options.DB})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("Redis outbox ping: %w", err)
	}
	base := options.Prefix + "{saga-outbox-v2}:"
	store := &RedisOutboxStore{client: client, base: base, markerKey: base + "schema", readyKey: base + "ready", legacyKey: options.Prefix + "saga-outbox:v2", ttl: options.TTL, maxRetries: options.MaxRetries, maxSagaBytes: options.MaxSagaBytes}
	legacy, err := client.Exists(ctx, store.legacyKey).Result()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	marker, markerErr := client.Get(ctx, store.markerKey).Result()
	if errors.Is(markerErr, redis.Nil) {
		marker = ""
	} else if markerErr != nil {
		_ = client.Close()
		return nil, markerErr
	}
	if legacy != 0 && marker != redisNormalizedMarker {
		_ = client.Close()
		return nil, fmt.Errorf("legacy Redis outbox data exists; run MigrateRedisOutboxV2 before startup")
	}
	if marker == "" {
		if err := client.SetNX(ctx, store.markerKey, redisNormalizedMarker, 0).Err(); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	return store, nil
}

// MigrateRedisOutboxV2 imports the former global document idempotently. The
// legacy key is retained as an operator-controlled backup.
func MigrateRedisOutboxV2(ctx context.Context, options RedisOutboxStoreOptions) error {
	if options.MaxLegacyBytes <= 0 {
		options.MaxLegacyBytes = 64 << 20
	}
	client := redis.NewClient(&redis.Options{Addr: options.Addr, Password: options.Password, DB: options.DB})
	defer client.Close()
	legacyKey := options.Prefix + "saga-outbox:v2"
	payload, err := client.Get(ctx, legacyKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(payload) > options.MaxLegacyBytes {
		return fmt.Errorf("legacy Redis outbox is %d bytes, limit is %d", len(payload), options.MaxLegacyBytes)
	}
	var legacy redisOutboxState
	if err := json.Unmarshal(payload, &legacy); err != nil {
		return fmt.Errorf("decode legacy Redis outbox: %w", err)
	}
	if legacy.Version != redisOutboxStateVersion {
		return fmt.Errorf("unsupported legacy Redis outbox version %d", legacy.Version)
	}
	legacy.initialize()
	if options.MaxRetries <= 0 {
		options.MaxRetries = 64
	}
	if options.MaxSagaBytes <= 0 {
		options.MaxSagaBytes = defaultRedisSagaBytes
	}
	base := options.Prefix + "{saga-outbox-v2}:"
	store := &RedisOutboxStore{client: client, base: base, markerKey: base + "schema", readyKey: base + "ready", legacyKey: legacyKey, ttl: options.TTL, maxRetries: options.MaxRetries, maxSagaBytes: options.MaxSagaBytes}
	sagaIDs := make([]string, 0, len(legacy.Sagas))
	for id := range legacy.Sagas {
		sagaIDs = append(sagaIDs, id)
	}
	sort.Strings(sagaIDs)
	for _, sagaID := range sagaIDs {
		state := newRedisOutboxState()
		state.Sagas[sagaID] = legacy.Sagas[sagaID]
		state.Steps[sagaID] = legacy.Steps[sagaID]
		for id, dispatch := range legacy.Dispatches {
			if dispatch.SagaID == sagaID {
				state.Dispatches[id] = dispatch
				state.Attempts[id] = legacy.Attempts[id]
			}
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			return err
		}
		existing, err := client.Get(ctx, store.sagaKey(sagaID)).Bytes()
		if err == nil {
			if string(existing) != string(encoded) {
				return fmt.Errorf("normalized saga %s already differs from legacy backup", sagaID)
			}
			continue
		}
		if !errors.Is(err, redis.Nil) {
			return err
		}
		if ok, err := store.casSaga(ctx, sagaID, nil, encoded, state); err != nil || !ok {
			if err != nil {
				return err
			}
			return ErrOptimisticConflict
		}
	}
	return client.Set(ctx, store.markerKey, redisNormalizedMarker, 0).Err()
}

func (store *RedisOutboxStore) OptimisticConflictRetries() uint64 {
	if store == nil {
		return 0
	}
	return store.conflicts.Load()
}
func (store *RedisOutboxStore) Close() error {
	if store == nil || store.client == nil {
		return nil
	}
	return store.client.Close()
}
func (store *RedisOutboxStore) sagaKey(id string) string     { return store.base + "saga:" + id }
func (store *RedisOutboxStore) dispatchKey(id string) string { return store.base + "dispatch:" + id }

func (store *RedisOutboxStore) CreateSaga(ctx context.Context, request CreateSagaRequest) (*SagaInstance, error) {
	value, err := store.mutateSaga(ctx, request.SagaID, func(memory *InMemoryOutboxStore) (any, error) { return memory.CreateSaga(ctx, request) })
	if err != nil {
		return nil, err
	}
	return value.(*SagaInstance), nil
}
func (store *RedisOutboxStore) EnqueueStep(ctx context.Context, request EnqueueStepRequest) (*Dispatch, error) {
	value, err := store.mutateSaga(ctx, request.SagaID, func(memory *InMemoryOutboxStore) (any, error) { return memory.EnqueueStep(ctx, request) })
	if err != nil {
		return nil, err
	}
	return value.(*Dispatch), nil
}
func (store *RedisOutboxStore) ClaimDispatch(ctx context.Context, options ClaimOptions) (*Dispatch, error) {
	if options.Owner == "" || options.LeaseDuration <= 0 {
		return nil, fmt.Errorf("claim owner and positive lease duration are required")
	}
	for retry := 0; retry < store.maxRetries; retry++ {
		var sagaID string
		if options.TargetDispatchID != "" {
			var err error
			sagaID, err = store.sagaForDispatch(ctx, options.TargetDispatchID)
			if err != nil {
				return nil, err
			}
		} else {
			now, err := store.client.Time(ctx).Result()
			if err != nil {
				return nil, err
			}
			ids, err := store.client.ZRangeByScore(ctx, store.readyKey, &redis.ZRangeBy{Min: "-inf", Max: fmt.Sprintf("%d", now.UnixMilli()), Offset: 0, Count: 1}).Result()
			if err != nil {
				return nil, err
			}
			if len(ids) == 0 {
				return nil, ErrNoDispatch
			}
			sagaID = ids[0]
		}
		value, err := store.mutateSaga(ctx, sagaID, func(memory *InMemoryOutboxStore) (any, error) { return memory.ClaimDispatch(ctx, options) })
		if err == nil {
			return value.(*Dispatch), nil
		}
		if errors.Is(err, ErrNoDispatch) || errors.Is(err, ErrOptimisticConflict) {
			continue
		}
		return nil, err
	}
	return nil, ErrOptimisticConflict
}
func (store *RedisOutboxStore) SaveFencingGrants(ctx context.Context, dispatchID string, attempt uint64, token string, grants []invocation.FencingGrant) error {
	saga, err := store.sagaForDispatch(ctx, dispatchID)
	if err != nil {
		return err
	}
	_, err = store.mutateSaga(ctx, saga, func(memory *InMemoryOutboxStore) (any, error) {
		return nil, memory.SaveFencingGrants(ctx, dispatchID, attempt, token, grants)
	})
	return err
}
func (store *RedisOutboxStore) CompleteDispatch(ctx context.Context, completion Completion) error {
	saga, err := store.sagaForDispatch(ctx, completion.DispatchID)
	if err != nil {
		return err
	}
	_, err = store.mutateSaga(ctx, saga, func(memory *InMemoryOutboxStore) (any, error) { return nil, memory.CompleteDispatch(ctx, completion) })
	return err
}
func (store *RedisOutboxStore) CompleteSaga(ctx context.Context, sagaID string) error {
	_, err := store.mutateSaga(ctx, sagaID, func(memory *InMemoryOutboxStore) (any, error) { return nil, memory.CompleteSaga(ctx, sagaID) })
	return err
}
func (store *RedisOutboxStore) GetSaga(ctx context.Context, sagaID string) (*SagaInstance, error) {
	memory, err := store.loadSaga(ctx, sagaID)
	if err != nil {
		return nil, err
	}
	return memory.GetSaga(ctx, sagaID)
}
func (store *RedisOutboxStore) GetDispatch(ctx context.Context, id string) (*Dispatch, error) {
	saga, err := store.sagaForDispatch(ctx, id)
	if err != nil {
		return nil, err
	}
	memory, err := store.loadSaga(ctx, saga)
	if err != nil {
		return nil, err
	}
	return memory.GetDispatch(ctx, id)
}
func (store *RedisOutboxStore) ListDispatches(ctx context.Context, sagaID string) ([]*Dispatch, error) {
	memory, err := store.loadSaga(ctx, sagaID)
	if err != nil {
		return nil, err
	}
	return memory.ListDispatches(ctx, sagaID)
}
func (store *RedisOutboxStore) ListAttempts(ctx context.Context, id string) ([]DispatchAttempt, error) {
	saga, err := store.sagaForDispatch(ctx, id)
	if err != nil {
		return nil, err
	}
	memory, err := store.loadSaga(ctx, saga)
	if err != nil {
		return nil, err
	}
	return memory.ListAttempts(ctx, id)
}

func (store *RedisOutboxStore) sagaForDispatch(ctx context.Context, id string) (string, error) {
	value, err := store.client.Get(ctx, store.dispatchKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return "", fmt.Errorf("dispatch not found: %s", id)
	}
	return value, err
}
func (store *RedisOutboxStore) loadSaga(ctx context.Context, id string) (*InMemoryOutboxStore, error) {
	payload, err := store.client.Get(ctx, store.sagaKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("saga not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	var state redisOutboxState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, err
	}
	if state.Version != redisOutboxStateVersion {
		return nil, fmt.Errorf("unsupported Redis saga version %d", state.Version)
	}
	now, err := store.client.Time(ctx).Result()
	if err != nil {
		return nil, err
	}
	return state.memory(func() time.Time { return now.UTC() }), nil
}

func (store *RedisOutboxStore) mutateSaga(ctx context.Context, sagaID string, operation func(*InMemoryOutboxStore) (any, error)) (any, error) {
	for retry := 0; retry < store.maxRetries; retry++ {
		var old []byte
		payload, err := store.client.Get(ctx, store.sagaKey(sagaID)).Bytes()
		state := newRedisOutboxState()
		if err == nil {
			old = payload
			if err := json.Unmarshal(payload, state); err != nil {
				return nil, err
			}
		} else if !errors.Is(err, redis.Nil) {
			return nil, err
		}
		now, err := store.client.Time(ctx).Result()
		if err != nil {
			return nil, err
		}
		memory := state.memory(func() time.Time { return now.UTC() })
		value, err := operation(memory)
		if err != nil {
			return nil, err
		}
		persisted := stateFromMemory(memory)
		encoded, err := json.Marshal(persisted)
		if err != nil {
			return nil, err
		}
		if len(encoded) > store.maxSagaBytes {
			return nil, fmt.Errorf("Redis saga %s is %d bytes, limit is %d", sagaID, len(encoded), store.maxSagaBytes)
		}
		ok, err := store.casSaga(ctx, sagaID, old, encoded, persisted)
		if err != nil {
			return nil, err
		}
		if ok {
			return value, nil
		}
		store.conflicts.Add(1)
	}
	return nil, fmt.Errorf("%w: Redis saga transaction exhausted retries", ErrOptimisticConflict)
}
func (store *RedisOutboxStore) casSaga(ctx context.Context, sagaID string, old, encoded []byte, state *redisOutboxState) (bool, error) {
	keys := []string{store.sagaKey(sagaID), store.readyKey}
	ids := make([]string, 0, len(state.Dispatches))
	for id := range state.Dispatches {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		keys = append(keys, store.dispatchKey(id))
	}
	score := ""
	if ready, ok := redisSagaReadyAt(state); ok {
		score = fmt.Sprintf("%d", ready.UnixMilli())
	}
	ttl := int64(0)
	if store.ttl > 0 && redisOutboxStateIsTerminal(state) {
		ttl = store.ttl.Milliseconds()
	}
	result, err := redisSagaCASScript.Run(ctx, store.client, keys, string(old), string(encoded), score, sagaID, ttl).Int()
	return result == 1, err
}
func redisSagaReadyAt(state *redisOutboxState) (time.Time, bool) {
	var earliest time.Time
	for _, d := range state.Dispatches {
		var at time.Time
		switch d.State {
		case DispatchQueued:
			at = d.CreatedAt
		case DispatchRetryWait:
			at = d.NextAttemptAt
		case DispatchInFlight:
			at = d.LeaseDeadline
		default:
			continue
		}
		if earliest.IsZero() || at.Before(earliest) {
			earliest = at
		}
	}
	return earliest, !earliest.IsZero()
}
func redisOutboxStateIsTerminal(state *redisOutboxState) bool {
	if state == nil || len(state.Sagas) == 0 {
		return false
	}
	for _, s := range state.Sagas {
		if s == nil || !isTerminalSaga(s.State) {
			return false
		}
	}
	return true
}
func newRedisOutboxState() *redisOutboxState {
	state := &redisOutboxState{Version: redisOutboxStateVersion}
	state.initialize()
	return state
}
func (state *redisOutboxState) initialize() {
	if state.Sagas == nil {
		state.Sagas = map[string]*SagaInstance{}
	}
	if state.Steps == nil {
		state.Steps = map[string]map[string]*SagaStep{}
	}
	if state.Dispatches == nil {
		state.Dispatches = map[string]*Dispatch{}
	}
	if state.Attempts == nil {
		state.Attempts = map[string][]DispatchAttempt{}
	}
}
func (state *redisOutboxState) memory(now func() time.Time) *InMemoryOutboxStore {
	state.initialize()
	return &InMemoryOutboxStore{sagas: state.Sagas, steps: state.Steps, dispatches: state.Dispatches, attempts: state.Attempts, now: now}
}
func stateFromMemory(memory *InMemoryOutboxStore) *redisOutboxState {
	return &redisOutboxState{Version: redisOutboxStateVersion, Sagas: memory.sagas, Steps: memory.steps, Dispatches: memory.dispatches, Attempts: memory.attempts}
}

var _ OutboxStore = (*RedisOutboxStore)(nil)
