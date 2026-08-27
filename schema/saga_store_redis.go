package schema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisSagaStoreOptions configures the Redis saga store.
type RedisSagaStoreOptions struct {
	Addr     string
	Password string
	DB       int
	Prefix   string
	TTL      time.Duration
}

// RedisSagaStore persists saga effects in Redis.
type RedisSagaStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewRedisSagaStore creates a Redis-backed saga store.
func NewRedisSagaStore(opts RedisSagaStoreOptions) (*RedisSagaStore, error) {
	if strings.TrimSpace(opts.Addr) == "" {
		return nil, fmt.Errorf("redis addr is required")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &RedisSagaStore{
		client: client,
		prefix: opts.Prefix,
		ttl:    opts.TTL,
	}, nil
}

func (rs *RedisSagaStore) StartTransaction(sagaID, ruleName string) error {
	if rs == nil || rs.client == nil {
		return fmt.Errorf("redis saga store not initialized")
	}
	if strings.TrimSpace(sagaID) == "" || strings.TrimSpace(ruleName) == "" {
		return fmt.Errorf("saga ID and rule name are required")
	}
	ctx := context.Background()
	key := rs.sagaKey(sagaID)
	return rs.watch(ctx, key, func(tx *redis.Tx) error {
		existing, err := tx.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		if len(existing) != 0 {
			if existing["rule"] != ruleName {
				return fmt.Errorf("saga identity conflict for %s", sagaID)
			}
			return nil
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, map[string]interface{}{
				"rule": ruleName, "status": "active",
				"created_at": time.Now().UTC().Format(time.RFC3339Nano), "completed": "",
			})
			// Active recovery state has no TTL. CompleteSaga starts retention.
			pipe.Persist(ctx, key)
			return nil
		})
		return err
	})
}

func (rs *RedisSagaStore) RecordEffect(sagaID, effectID string, sequence int, verb string, args map[string]interface{}) error {
	if rs == nil || rs.client == nil {
		return fmt.Errorf("redis saga store not initialized")
	}
	ctx := context.Background()
	effect := &SagaEffect{
		ID:        effectID,
		Sequence:  sequence,
		Verb:      verb,
		Args:      args,
		Status:    SagaEffectPending,
		Timestamp: time.Now().UTC(),
	}
	payload, err := json.Marshal(effect)
	if err != nil {
		return fmt.Errorf("marshal saga effect: %w", err)
	}
	_, requestedHash, err := CanonicalJSON(args)
	if err != nil {
		return err
	}
	key := rs.effectsKey(sagaID)
	return rs.watchKeys(ctx, []string{rs.sagaKey(sagaID), key}, func(tx *redis.Tx) error {
		status, err := tx.HGet(ctx, rs.sagaKey(sagaID), "status").Result()
		if err != nil {
			return fmt.Errorf("read saga status: %w", err)
		}
		if status != "active" {
			return fmt.Errorf("saga %s is not active", sagaID)
		}
		values, err := tx.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			return err
		}
		for _, entry := range values {
			var existing SagaEffect
			if err := json.Unmarshal([]byte(entry), &existing); err != nil {
				return err
			}
			if existing.ID != effectID {
				continue
			}
			_, existingHash, err := CanonicalJSON(existing.Args)
			if err != nil {
				return err
			}
			if existing.Sequence == sequence && existing.Verb == verb && existingHash == requestedHash {
				return nil
			}
			return fmt.Errorf("effect identity conflict for saga %s effect %s", sagaID, effectID)
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.RPush(ctx, key, payload)
			pipe.Persist(ctx, key)
			pipe.Persist(ctx, rs.sagaKey(sagaID))
			return nil
		})
		return err
	})
}

func (rs *RedisSagaStore) MarkSuccess(sagaID, effectID string, result interface{}) error {
	return rs.updateEffectStatus(sagaID, effectID, SagaEffectSuccess, "", result)
}

func (rs *RedisSagaStore) MarkFailed(sagaID, effectID string, reason error) error {
	msg := ""
	if reason != nil {
		msg = reason.Error()
	}
	return rs.updateEffectStatus(sagaID, effectID, SagaEffectFailed, msg, nil)
}

func (rs *RedisSagaStore) MarkCompensated(sagaID, effectID string) error {
	return rs.updateEffectStatus(sagaID, effectID, SagaEffectCompensated, "", nil)
}

func (rs *RedisSagaStore) GetTransactionEffects(sagaID string) ([]*SagaEffect, error) {
	if rs == nil || rs.client == nil {
		return nil, fmt.Errorf("redis saga store not initialized")
	}
	ctx := context.Background()
	values, err := rs.client.LRange(ctx, rs.effectsKey(sagaID), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	effects := make([]*SagaEffect, 0, len(values))
	for _, entry := range values {
		var effect SagaEffect
		if err := json.Unmarshal([]byte(entry), &effect); err != nil {
			return nil, err
		}
		effects = append(effects, &effect)
	}
	return effects, nil
}

func (rs *RedisSagaStore) GetActiveSagas() ([]string, error) {
	if rs == nil || rs.client == nil {
		return nil, fmt.Errorf("redis saga store not initialized")
	}
	ctx := context.Background()
	var cursor uint64
	pattern := rs.prefix + "saga:*"
	active := []string{}
	for {
		keys, next, err := rs.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			if strings.HasSuffix(key, ":effects") {
				continue
			}
			status, _ := rs.client.HGet(ctx, key, "status").Result()
			completed, _ := rs.client.HGet(ctx, key, "completed").Result()
			if status == "completed" || completed != "" {
				continue
			}
			sagaID := strings.TrimPrefix(key, rs.prefix+"saga:")
			active = append(active, sagaID)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	sort.Strings(active)
	return active, nil
}

func (rs *RedisSagaStore) CompleteSaga(sagaID string) error {
	if rs == nil || rs.client == nil {
		return fmt.Errorf("redis saga store not initialized")
	}
	ctx := context.Background()
	key := rs.sagaKey(sagaID)
	return rs.watchKeys(ctx, []string{key, rs.effectsKey(sagaID)}, func(tx *redis.Tx) error {
		exists, err := tx.Exists(ctx, key).Result()
		if err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("saga not found: %s", sagaID)
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, map[string]interface{}{
				"status": "completed", "completed": time.Now().UTC().Format(time.RFC3339Nano),
			})
			if rs.ttl > 0 {
				pipe.PExpire(ctx, key, rs.ttl)
				pipe.PExpire(ctx, rs.effectsKey(sagaID), rs.ttl)
			}
			return nil
		})
		return err
	})
}

func (rs *RedisSagaStore) updateEffectStatus(sagaID, effectID, status, errMsg string, result interface{}) error {
	if rs == nil || rs.client == nil {
		return fmt.Errorf("redis saga store not initialized")
	}
	ctx := context.Background()
	key := rs.effectsKey(sagaID)
	return rs.watchKeys(ctx, []string{rs.sagaKey(sagaID), key}, func(tx *redis.Tx) error {
		statusValue, err := tx.HGet(ctx, rs.sagaKey(sagaID), "status").Result()
		if err != nil {
			return fmt.Errorf("read saga status: %w", err)
		}
		if statusValue != "active" {
			return fmt.Errorf("saga %s is not active", sagaID)
		}
		values, err := tx.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			return err
		}
		if len(values) == 0 {
			return fmt.Errorf("no effects found for saga %s", sagaID)
		}
		updated := append([]string(nil), values...)
		matched := false
		for i, entry := range values {
			var effect SagaEffect
			if err := json.Unmarshal([]byte(entry), &effect); err != nil {
				return err
			}
			if effect.ID != effectID {
				continue
			}
			if !validSagaStatusTransition(effect.Status, status) {
				return fmt.Errorf("cannot transition saga %s effect %s from %s to %s", sagaID, effectID, effect.Status, status)
			}
			effect.Status = status
			effect.Error = errMsg
			if status == SagaEffectSuccess {
				effect.Result = result
			}
			payload, err := json.Marshal(effect)
			if err != nil {
				return err
			}
			updated[i] = string(payload)
			matched = true
			break
		}
		if !matched {
			return fmt.Errorf("effect not found for saga %s: %s", sagaID, effectID)
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Del(ctx, key)
			for _, entry := range updated {
				pipe.RPush(ctx, key, entry)
			}
			pipe.Persist(ctx, key)
			pipe.Persist(ctx, rs.sagaKey(sagaID))
			return nil
		})
		return err
	})
}

func validSagaStatusTransition(current, next string) bool {
	if current == next {
		return true
	}
	switch next {
	case SagaEffectSuccess, SagaEffectFailed:
		return current == SagaEffectPending
	case SagaEffectCompensated:
		return current == SagaEffectSuccess
	default:
		return false
	}
}

// Close releases the Redis client resources.
func (rs *RedisSagaStore) Close() error {
	if rs == nil || rs.client == nil {
		return nil
	}
	return rs.client.Close()
}

func (rs *RedisSagaStore) watch(ctx context.Context, key string, operation func(*redis.Tx) error) error {
	return rs.watchKeys(ctx, []string{key}, operation)
}

func (rs *RedisSagaStore) watchKeys(ctx context.Context, keys []string, operation func(*redis.Tx) error) error {
	for retry := 0; retry < 64; retry++ {
		err := rs.client.Watch(ctx, operation, keys...)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	return fmt.Errorf("Redis saga optimistic transaction exhausted retries")
}

func (rs *RedisSagaStore) sagaKey(sagaID string) string {
	return rs.prefix + "saga:" + sagaID
}

func (rs *RedisSagaStore) effectsKey(sagaID string) string {
	return rs.prefix + "saga:" + sagaID + ":effects"
}
