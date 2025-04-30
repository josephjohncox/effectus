package eval

import (
	"fmt"

	"github.com/effectus/effectus-go"
)

// Predicate represents a condition to be evaluated against facts
type Predicate struct {
	Path string      // The fact path to get the value from
	Op   string      // The comparison operator (==, !=, <, >, etc.)
	Lit  interface{} // The literal value to compare against
}

// EvaluatePredicates evaluates a slice of predicates against facts
func EvaluatePredicates(predicates []*Predicate, facts effectus.Facts) bool {
	if len(predicates) == 0 {
		return true
	}

	for _, pred := range predicates {
		// Get fact value
		value, exists := facts.Get(pred.Path)
		if !exists {
			return false
		}

		// Compare based on operator
		if !CompareFact(value, pred.Op, pred.Lit) {
			return false
		}
	}

	return true
}

// CompareFact compares a fact value to a literal using the given operator
func CompareFact(factValue interface{}, op string, literal interface{}) bool {
	switch op {
	case "==":
		return Equal(factValue, literal)
	case "!=":
		return !Equal(factValue, literal)
	case "<":
		return LessThan(factValue, literal)
	case "<=":
		return LessThan(factValue, literal) || Equal(factValue, literal)
	case ">":
		return GreaterThan(factValue, literal)
	case ">=":
		return GreaterThan(factValue, literal) || Equal(factValue, literal)
	case "in":
		return Contains(literal, factValue)
	case "contains":
		return Contains(factValue, literal)
	default:
		return false
	}
}

// Equal compares two values for equality
func Equal(a, b interface{}) bool {
	// Handle string comparison with quotes
	if aStr, aOk := a.(string); aOk {
		if bStr, bOk := b.(string); bOk {
			// Remove quotes if present
			if len(bStr) >= 2 && bStr[0] == '"' && bStr[len(bStr)-1] == '"' {
				unquoted := bStr[1 : len(bStr)-1]
				return aStr == unquoted
			}
			return aStr == bStr
		}
	}

	// Handle numeric comparisons
	switch aVal := a.(type) {
	case float64:
		switch bVal := b.(type) {
		case float64:
			return aVal == bVal
		case int:
			return aVal == float64(bVal)
		}
	case int:
		switch bVal := b.(type) {
		case float64:
			return float64(aVal) == bVal
		case int:
			return aVal == bVal
		}
	}

	// Basic equality comparison
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// LessThan checks if a is less than b
func LessThan(a, b interface{}) bool {
	// Handle numeric comparisons
	switch aVal := a.(type) {
	case float64:
		switch bVal := b.(type) {
		case float64:
			return aVal < bVal
		case int:
			return aVal < float64(bVal)
		}
	case int:
		switch bVal := b.(type) {
		case float64:
			return float64(aVal) < bVal
		case int:
			return aVal < bVal
		}
	}

	// Simple string comparison as a fallback
	return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b)
}

// GreaterThan checks if a is greater than b
func GreaterThan(a, b interface{}) bool {
	// Handle numeric comparisons
	switch aVal := a.(type) {
	case float64:
		switch bVal := b.(type) {
		case float64:
			return aVal > bVal
		case int:
			return aVal > float64(bVal)
		}
	case int:
		switch bVal := b.(type) {
		case float64:
			return float64(aVal) > bVal
		case int:
			return aVal > bVal
		}
	}

	// Simple string comparison as a fallback
	return fmt.Sprintf("%v", a) > fmt.Sprintf("%v", b)
}

// Contains checks if container contains item
func Contains(container, item interface{}) bool {
	// Check if container contains item
	switch c := container.(type) {
	case []interface{}:
		for _, v := range c {
			if Equal(v, item) {
				return true
			}
		}
	case string:
		if s, ok := item.(string); ok {
			// Properly check for substring containment
			for i := 0; i <= len(c)-len(s); i++ {
				if i+len(s) <= len(c) && c[i:i+len(s)] == s {
					return true
				}
			}
		}
	}
	return false
}
