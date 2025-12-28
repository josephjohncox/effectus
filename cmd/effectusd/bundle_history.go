package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/unified"
)

type bundleHistory struct {
	mu    sync.RWMutex
	limit int
	dir   string
	items []bundleSnapshot
}

type bundleSnapshot struct {
	ID        string
	Reason    string
	CreatedAt time.Time
	Path      string
	Bundle    *unified.Bundle
}

type bundleSnapshotSummary struct {
	ID        string    `json:"id"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	Path      string    `json:"path,omitempty"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Rules     int       `json:"rules"`
	Flows     int       `json:"flows"`
}

type bundleSnapshotFile struct {
	Meta     bundleSnapshotSummary `json:"meta"`
	Bundle   *unified.Bundle       `json:"bundle"`
	ListSpec *list.Spec            `json:"list_spec,omitempty"`
	FlowSpec *flow.Spec            `json:"flow_spec,omitempty"`
}

func newBundleHistory(limit int, dir string) *bundleHistory {
	if limit <= 0 {
		return nil
	}
	return &bundleHistory{limit: limit, dir: dir}
}

func (h *bundleHistory) Add(bundle *unified.Bundle, reason string) (*bundleSnapshot, error) {
	if h == nil || bundle == nil {
		return nil, nil
	}
	id, _ := generateToken()
	if id == "" {
		id = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	snapshot := bundleSnapshot{
		ID:        id,
		Reason:    reason,
		CreatedAt: time.Now(),
		Bundle:    bundle,
	}
	if h.dir != "" {
		if err := os.MkdirAll(h.dir, 0755); err == nil {
			filename := fmt.Sprintf("bundle-%s.json", id[:8])
			path := filepath.Join(h.dir, filename)
			if err := writeBundleSnapshot(path, snapshot); err == nil {
				snapshot.Path = path
			}
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.items = append([]bundleSnapshot{snapshot}, h.items...)
	if len(h.items) > h.limit {
		trimmed := h.items[h.limit:]
		h.items = h.items[:h.limit]
		for _, old := range trimmed {
			if old.Path != "" {
				_ = os.Remove(old.Path)
			}
		}
	}

	return &snapshot, nil
}

func (h *bundleHistory) List() []bundleSnapshotSummary {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]bundleSnapshotSummary, 0, len(h.items))
	for _, item := range h.items {
		out = append(out, summarizeSnapshot(item))
	}
	return out
}

func (h *bundleHistory) Get(id string) (*bundleSnapshot, bool) {
	if h == nil || id == "" {
		return nil, false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, item := range h.items {
		if item.ID == id {
			return &item, true
		}
	}
	return nil, false
}

func summarizeSnapshot(snapshot bundleSnapshot) bundleSnapshotSummary {
	name := ""
	version := ""
	rules := 0
	flows := 0
	if snapshot.Bundle != nil {
		name = snapshot.Bundle.Name
		version = snapshot.Bundle.Version
		rules = countRules(snapshot.Bundle)
		flows = countFlows(snapshot.Bundle)
	}
	return bundleSnapshotSummary{
		ID:        snapshot.ID,
		Reason:    snapshot.Reason,
		CreatedAt: snapshot.CreatedAt,
		Path:      snapshot.Path,
		Name:      name,
		Version:   version,
		Rules:     rules,
		Flows:     flows,
	}
}

func ptrSnapshotSummary(snapshot *bundleSnapshot) *bundleSnapshotSummary {
	if snapshot == nil {
		return nil
	}
	sum := summarizeSnapshot(*snapshot)
	return &sum
}

func writeBundleSnapshot(path string, snapshot bundleSnapshot) error {
	if snapshot.Bundle == nil {
		return fmt.Errorf("bundle missing")
	}
	payload := bundleSnapshotFile{
		Meta:     summarizeSnapshot(snapshot),
		Bundle:   snapshot.Bundle,
		ListSpec: snapshot.Bundle.ListSpec,
		FlowSpec: snapshot.Bundle.FlowSpec,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
