package verb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

)

// Registry manages verb specifications and their implementations
type Registry struct {
	// verbs is a map of verb name to specification
	verbs map[string]*Spec

	// typeSystem is a generic interface to the type system
	typeSystem interface{}

	// mu protects concurrent access
	mu sync.RWMutex

	// verbHash is a hash of all registered verbs
	verbHash string
}

// NewRegistry creates a new verb registry
func NewRegistry(typeSystem interface{}) *Registry {
	return &Registry{
		verbs:      make(map[string]*Spec),
		typeSystem: typeSystem,
	}
}

// RegisterVerb registers a verb specification
func (r *Registry) RegisterVerb(spec *Spec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if verb already exists
	if existing, exists := r.verbs[spec.Name]; exists {
		return fmt.Errorf("verb '%s' already registered with capability %s", spec.Name, existing.Capability)
	}

	// Register the verb
	r.verbs[spec.Name] = spec

	// Invalidate the hash
	r.verbHash = ""

	return nil
}

// GetVerb retrieves a verb specification by name
func (r *Registry) GetVerb(name string) (*Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	spec, exists := r.verbs[name]
	return spec, exists
}

// SetExecutor sets the executor for a registered verb
func (r *Registry) SetExecutor(name string, executor Executor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	spec, exists := r.verbs[name]
	if !exists {
		return fmt.Errorf("verb '%s' not registered", name)
	}

	spec.Executor = executor
	return nil
}

// GetVerbHash returns a hash of all registered verbs
func (r *Registry) GetVerbHash() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return cached hash if available
	if r.verbHash != "" {
		return r.verbHash
	}

	// Collect verbs for hashing
	type verbHashInfo struct {
		Name        string                 `json:"name"`
		ArgTypes    map[string]interface{} `json:"arg_types"`
		ReturnType  interface{}            `json:"return_type"`
		Capability  Capability             `json:"capability"`
		Inverse     string                 `json:"inverse,omitempty"`
		Description string                 `json:"description,omitempty"`
	}

	verbInfos := make([]verbHashInfo, 0, len(r.verbs))
	for _, spec := range r.verbs {
		verbInfos = append(verbInfos, verbHashInfo{
			Name:        spec.Name,
			ArgTypes:    spec.ArgTypes,
			ReturnType:  spec.ReturnType,
			Capability:  spec.Capability,
			Inverse:     spec.Inverse,
			Description: spec.Description,
		})
	}

	// Sort by name for consistent hashing
	sort.Slice(verbInfos, func(i, j int) bool {
		return verbInfos[i].Name < verbInfos[j].Name
	})

	// Marshal to JSON and hash
	jsonData, err := json.Marshal(verbInfos)
	if err != nil {
		// If marshaling fails, use a default hash
		r.verbHash = "error-computing-hash"
		return r.verbHash
	}

	hash := sha256.Sum256(jsonData)
	r.verbHash = hex.EncodeToString(hash[:])

	return r.verbHash
}

// GetAllVerbs returns all registered verbs
func (r *Registry) GetAllVerbs() []*Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	verbs := make([]*Spec, 0, len(r.verbs))
	for _, spec := range r.verbs {
		verbs = append(verbs, spec)
	}

	// Sort by name for consistent ordering
	sort.Slice(verbs, func(i, j int) bool {
		return verbs[i].Name < verbs[j].Name
	})

	return verbs
}

// GetTypeSystem returns the type system
func (r *Registry) GetTypeSystem() interface{} {
	return r.typeSystem
}

// Count returns the number of registered verbs
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.verbs)
}
