package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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
	ctx := context.Background()
	key := rs.sagaKey(sagaID)
	values := map[string]interface{}{
		"rule":       ruleName,
		"status":     "active",
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		"completed":  "",
	}
	if err := rs.client.HSet(ctx, key, values).Err(); err != nil {
		return err
	}
	rs.applyTTL(ctx, key)
	return nil
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
	key := rs.effectsKey(sagaID)
	return rs.client.Watch(ctx, func(tx *redis.Tx) error {
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
			if existing.Sequence == sequence && existing.Verb == verb && reflect.DeepEqual(existing.Args, args) {
				return nil
			}
			return fmt.Errorf("effect identity conflict for saga %s effect %s", sagaID, effectID)
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.RPush(ctx, key, payload)
			if rs.ttl > 0 {
				pipe.Expire(ctx, key, rs.ttl)
			}
			return nil
		})
		return err
	}, key)
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
	return active, nil
}

func (rs *RedisSagaStore) CompleteSaga(sagaID string) error {
	if rs == nil || rs.client == nil {
		return fmt.Errorf("redis saga store not initialized")
	}
	ctx := context.Background()
	key := rs.sagaKey(sagaID)
	if err := rs.client.HSet(ctx, key, map[string]interface{}{
		"status":    "completed",
		"completed": time.Now().UTC().Format(time.RFC3339Nano),
	}).Err(); err != nil {
		return err
	}
	rs.applyTTL(ctx, key)
	return nil
}

func (rs *RedisSagaStore) updateEffectStatus(sagaID, effectID, status, errMsg string, result interface{}) error {
	if rs == nil || rs.client == nil {
		return fmt.Errorf("redis saga store not initialized")
	}
	ctx := context.Background()
	key := rs.effectsKey(sagaID)
	return rs.client.Watch(ctx, func(tx *redis.Tx) error {
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
			if rs.ttl > 0 {
				pipe.Expire(ctx, key, rs.ttl)
			}
			return nil
		})
		return err
	}, key)
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

func (rs *RedisSagaStore) sagaKey(sagaID string) string {
	return rs.prefix + "saga:" + sagaID
}

func (rs *RedisSagaStore) effectsKey(sagaID string) string {
	return rs.prefix + "saga:" + sagaID + ":effects"
}

func (rs *RedisSagaStore) applyTTL(ctx context.Context, key string) {
	if rs == nil || rs.ttl <= 0 {
		return
	}
	_ = rs.client.Expire(ctx, key, rs.ttl).Err()
}
