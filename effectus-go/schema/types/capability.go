package types

// Capability represents the capability required by a verb
type Capability uint32

const (
	// CapabilityNone represents no capability
	CapabilityNone Capability = iota
	// CapabilityRead represents read-only access
	CapabilityRead
	// CapabilityModify represents modification access
	CapabilityModify
	// CapabilityCreate represents creation access
	CapabilityCreate
	// CapabilityDelete represents deletion access
	CapabilityDelete
)

// String returns a string representation of the capability
func (c Capability) String() string {
	switch c {
	case CapabilityNone:
		return "none"
	case CapabilityRead:
		return "read"
	case CapabilityModify:
		return "modify"
	case CapabilityCreate:
		return "create"
	case CapabilityDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// CanPerform checks if this capability can perform the specified capability
// Implements a simple capability lattice where higher capabilities include lower ones
func (c Capability) CanPerform(required Capability) bool {
	// None capability can't do anything
	if c == CapabilityNone {
		return required == CapabilityNone
	}

	// Special case: Delete is highest but doesn't imply modify/create
	if c == CapabilityDelete {
		return required == CapabilityDelete || required == CapabilityRead || required == CapabilityNone
	}

	// Read only allows read
	if c == CapabilityRead {
		return required == CapabilityRead || required == CapabilityNone
	}

	// Modify implies read
	if c == CapabilityModify {
		return required == CapabilityModify || required == CapabilityRead || required == CapabilityNone
	}

	// Create implies read (but not modify)
	if c == CapabilityCreate {
		return required == CapabilityCreate || required == CapabilityRead || required == CapabilityNone
	}

	return false
}
