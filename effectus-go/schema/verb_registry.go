// schema/verb_registry.go
package schema

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// VerbCapability defines what a verb is allowed to do
type VerbCapability int

const (
	CapReadOnly VerbCapability = iota
	CapModify
	CapCreateResource
	CapDeleteResource
)

// VerbExecutor defines the interface for verb implementations
type VerbExecutor interface {
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// VerbSpec defines the complete specification for a verb
type VerbSpec struct {
	Name        string           `json:"name"`
	ArgTypes    map[string]*Type `json:"arg_types"`
	ReturnType  *Type            `json:"return_type"`
	Capability  VerbCapability   `json:"capability"`
	Inverse     string           `json:"inverse,omitempty"`
	Description string           `json:"description,omitempty"`
	Executor    VerbExecutor     `json:"-"`
}

// VerbRegistry manages verb specifications and executors
type VerbRegistry struct {
	verbs      map[string]*VerbSpec
	verbHash   string
	typeSystem *TypeSystem
}

// NewVerbRegistry creates a new verb registry
func NewVerbRegistry(ts *TypeSystem) *VerbRegistry {
	return &VerbRegistry{
		verbs:      make(map[string]*VerbSpec),
		typeSystem: ts,
	}
}

// RegisterVerb registers a verb with the registry
func (vr *VerbRegistry) RegisterVerb(spec *VerbSpec) error {
	if _, exists := vr.verbs[spec.Name]; exists {
		return fmt.Errorf("verb %s already registered", spec.Name)
	}

	// Validate return type exists in type system
	if spec.ReturnType.PrimType == TypeUnknown && spec.ReturnType.Name != "" {
		if _, exists := vr.typeSystem.FactTypes[spec.ReturnType.Name]; !exists {
			// Check if it's a known type name
			knownType := false
			for _, typ := range vr.typeSystem.FactTypes {
				if typ.Name == spec.ReturnType.Name {
					knownType = true
					break
				}
			}

			if !knownType {
				return fmt.Errorf("return type %s is not registered in type system", spec.ReturnType.Name)
			}
		}
	}

	// Validate arg types
	for argName, argType := range spec.ArgTypes {
		if argType.PrimType == TypeUnknown && argType.Name != "" {
			if _, exists := vr.typeSystem.FactTypes[argType.Name]; !exists {
				// Check if it's a known type name
				knownType := false
				for _, typ := range vr.typeSystem.FactTypes {
					if typ.Name == argType.Name {
						knownType = true
						break
					}
				}

				if !knownType {
					return fmt.Errorf("arg type %s for %s is not registered in type system", argType.Name, argName)
				}
			}
		}
	}

	vr.verbs[spec.Name] = spec
	vr.updateVerbHash()

	return nil
}

// GetVerb retrieves a verb specification
func (vr *VerbRegistry) GetVerb(name string) (*VerbSpec, bool) {
	verb, exists := vr.verbs[name]
	return verb, exists
}

// GetVerbHash returns the hash of all registered verbs
func (vr *VerbRegistry) GetVerbHash() string {
	return vr.verbHash
}

// updateVerbHash updates the hash of all registered verbs
func (vr *VerbRegistry) updateVerbHash() {
	// Sort verb names for deterministic hash
	verbNames := make([]string, 0, len(vr.verbs))
	for name := range vr.verbs {
		verbNames = append(verbNames, name)
	}
	sort.Strings(verbNames)

	// Create a hash of all registered verbs (excluding executors)
	hasher := sha256.New()
	for _, name := range verbNames {
		verb := vr.verbs[name]

		// Create a JSON-serializable version without executor
		specForHash := struct {
			Name        string           `json:"name"`
			ArgTypes    map[string]*Type `json:"arg_types"`
			ReturnType  *Type            `json:"return_type"`
			Capability  VerbCapability   `json:"capability"`
			Inverse     string           `json:"inverse,omitempty"`
			Description string           `json:"description,omitempty"`
		}{
			Name:        verb.Name,
			ArgTypes:    verb.ArgTypes,
			ReturnType:  verb.ReturnType,
			Capability:  verb.Capability,
			Inverse:     verb.Inverse,
			Description: verb.Description,
		}

		// Serialize and hash
		specBytes, _ := json.Marshal(specForHash)
		hasher.Write(specBytes)
	}

	vr.verbHash = hex.EncodeToString(hasher.Sum(nil))
}
