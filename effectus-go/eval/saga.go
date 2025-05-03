// eval/saga.go
package eval

import (
	"fmt"
	"sync"
	"time"
)

// MemorySagaStore implements an in-memory saga store
type MemorySagaStore struct {
	mu      sync.RWMutex
	effects map[string][]sagaEffect
	txNames map[string]string // Maps txID to rule name
}

type sagaEffect struct {
	TxID      string
	Seq       int
	Verb      string
	Args      map[string]interface{}
	Status    string
	Timestamp time.Time
}

// RedisOptions contains configuration for Redis saga store
type RedisOptions struct {
	Addr     string
	Password string
	DB       int
}

// PostgresOptions contains configuration for Postgres saga store
type PostgresOptions struct {
	ConnString string
}

// NewMemorySagaStore creates a new in-memory saga store
func NewMemorySagaStore() *MemorySagaStore {
	return &MemorySagaStore{
		effects: make(map[string][]sagaEffect),
		txNames: make(map[string]string),
	}
}

// StartTransaction starts a new transaction
func (s *MemorySagaStore) StartTransaction(ruleName string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate a unique transaction ID (in a real implementation, this would be more robust)
	txID := fmt.Sprintf("tx-%d", time.Now().UnixNano())
	s.txNames[txID] = ruleName
	return txID, nil
}

// RecordEffect records an effect in the transaction
func (s *MemorySagaStore) RecordEffect(txID, verb string, args map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if transaction exists
	if _, exists := s.txNames[txID]; !exists {
		return fmt.Errorf("transaction %s not found", txID)
	}

	// Determine the sequence number
	seq := len(s.effects[txID])

	s.effects[txID] = append(s.effects[txID], sagaEffect{
		TxID:      txID,
		Seq:       seq,
		Verb:      verb,
		Args:      args,
		Status:    "pending",
		Timestamp: time.Now(),
	})

	return nil
}

// MarkSuccess marks an effect as successful
func (s *MemorySagaStore) MarkSuccess(txID, verb string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find the last effect with the given verb and mark it successful
	txEffects := s.effects[txID]
	for i := len(txEffects) - 1; i >= 0; i-- {
		if txEffects[i].Verb == verb && txEffects[i].Status == "pending" {
			txEffects[i].Status = "success"
			return nil
		}
	}

	return fmt.Errorf("pending effect with verb %s not found in transaction %s", verb, txID)
}

// MarkCompensated marks an effect as compensated
func (s *MemorySagaStore) MarkCompensated(txID, verb string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find the last successful effect with the given verb and mark it compensated
	txEffects := s.effects[txID]
	for i := len(txEffects) - 1; i >= 0; i-- {
		if txEffects[i].Verb == verb && txEffects[i].Status == "success" {
			txEffects[i].Status = "compensated"
			return nil
		}
	}

	return fmt.Errorf("successful effect with verb %s not found in transaction %s", verb, txID)
}

// GetTransactionEffects retrieves all effects for a transaction
func (s *MemorySagaStore) GetTransactionEffects(txID string) ([]TransactionEffect, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txEffects, ok := s.effects[txID]
	if !ok {
		return nil, fmt.Errorf("transaction %s not found", txID)
	}

	results := make([]TransactionEffect, len(txEffects))
	for i, effect := range txEffects {
		results[i] = TransactionEffect{
			TxID:   effect.TxID,
			Verb:   effect.Verb,
			Args:   effect.Args,
			Status: effect.Status,
		}
	}

	return results, nil
}

// NewRedisSagaStore creates a new Redis-backed saga store
func NewRedisSagaStore(opts RedisOptions) SagaStore {
	// In a real implementation, this would initialize a Redis client
	// For now, just return a memory store
	return NewMemorySagaStore()
}

// NewPostgresSagaStore creates a new Postgres-backed saga store
func NewPostgresSagaStore(opts PostgresOptions) SagaStore {
	// In a real implementation, this would initialize a database connection
	// For now, just return a memory store
	return NewMemorySagaStore()
}

// SagaStore defines the interface for saga transaction storage
type SagaStore interface {
	StartTransaction(ruleName string) (string, error)
	RecordEffect(txID, verb string, args map[string]interface{}) error
	MarkSuccess(txID, verb string) error
	MarkCompensated(txID, verb string) error
	GetTransactionEffects(txID string) ([]TransactionEffect, error)
}

// TransactionEffect represents an effect executed as part of a transaction
type TransactionEffect struct {
	TxID   string
	Verb   string
	Args   map[string]interface{}
	Status string // "pending", "success", "failed", "compensated"
}