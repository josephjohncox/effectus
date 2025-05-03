// Package verb provides definitions and utilities for effect verbs
package verb

import (
	"fmt"
	"strings"
)

// Capability represents a capability flag for a verb
type Capability uint64

const (
	// Basic capability types
	CapNone   Capability = 0
	CapRead   Capability = 1 << iota // Read-only capability
	CapWrite                         // Write capability
	CapCreate                        // Create resource capability
	CapDelete                        // Delete resource capability

	// Semantic properties
	CapIdempotent  // Operation can be repeated without side effects
	CapExclusive   // Operation should be exclusive (no concurrent operations)
	CapCommutative // Operation order doesn't matter with other commutative ops
	CapDefault     // Default capability for verbs with no specific requirements

	// Common combinations
	CapReadWrite = CapRead | CapWrite
	CapAll       = CapRead | CapWrite | CapCreate | CapDelete
)

// ResourceCapability pairs a resource name with capabilities
type ResourceCapability struct {
	Resource string     // Resource identifier (e.g., "order", "customer")
	Cap      Capability // Capabilities required for this resource
}

// String returns a string representation of the capability
func (c Capability) String() string {
	if c == CapNone {
		return "NONE"
	}

	var parts []string

	// Check basic capabilities
	if c&CapRead != 0 {
		parts = append(parts, "READ")
	}
	if c&CapWrite != 0 {
		parts = append(parts, "WRITE")
	}
	if c&CapCreate != 0 {
		parts = append(parts, "CREATE")
	}
	if c&CapDelete != 0 {
		parts = append(parts, "DELETE")
	}

	// Check semantic properties
	if c&CapIdempotent != 0 {
		parts = append(parts, "IDEMPOTENT")
	}
	if c&CapExclusive != 0 {
		parts = append(parts, "EXCLUSIVE")
	}
	if c&CapCommutative != 0 {
		parts = append(parts, "COMMUTATIVE")
	}
	if c&CapDefault != 0 {
		parts = append(parts, "DEFAULT")
	}

	return strings.Join(parts, "|")
}

// CanAccess checks if one capability can access resources protected by another
// Following the principle of least privilege
func (c Capability) CanAccess(required Capability) bool {
	// Basic access check: all required capabilities must be present
	return (c & required) == required
}

// IsCommutativeWith checks if two operations can be reordered
func (c Capability) IsCommutativeWith(other Capability) bool {
	// If both are commutative
	if c&CapCommutative != 0 && other&CapCommutative != 0 {
		return true
	}

	// Read operations are commutative with each other
	if c&CapWrite == 0 && other&CapWrite == 0 {
		return true
	}

	return false
}

// ResourceConflict checks if two resource capabilities conflict
func ResourceConflict(a, b ResourceCapability) bool {
	// If they target different resources, no conflict
	if a.Resource != b.Resource {
		return false
	}

	// If both are read-only, no conflict
	if a.Cap&CapWrite == 0 && b.Cap&CapWrite == 0 {
		return false
	}

	// If both are commutative, no conflict
	if a.Cap.IsCommutativeWith(b.Cap) {
		return false
	}

	// Otherwise, they conflict (write + anything on same resource)
	return true
}

// ResourceSet represents multiple resources with capabilities
type ResourceSet []ResourceCapability

// Conflicts checks if two resource sets have conflicts
func (rs ResourceSet) ConflictsWith(other ResourceSet) bool {
	for _, r1 := range rs {
		for _, r2 := range other {
			if ResourceConflict(r1, r2) {
				return true
			}
		}
	}
	return false
}

// String returns a string representation of the resource set
func (rs ResourceSet) String() string {
	if len(rs) == 0 {
		return "[]"
	}

	var parts []string
	for _, rc := range rs {
		parts = append(parts, fmt.Sprintf("%s:%s", rc.Resource, rc.Cap))
	}

	return fmt.Sprintf("[%s]", strings.Join(parts, ", "))
}
