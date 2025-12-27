package common

import (
	"github.com/effectus/effectus-go/pathutil"
)

// Predicate represents a compiled condition on facts
type Predicate struct {
	Path pathutil.Path // The path in the facts to check
	Op   string        // The operator (==, !=, >, <, >=, <=, in, contains)
	Lit  interface{}   // The literal value to compare against
}

// A shared type for predicates that can be used by both eval and list packages
// This prevents circular dependencies
