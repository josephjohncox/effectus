package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/effectus/effectus-go/pathutil"
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
