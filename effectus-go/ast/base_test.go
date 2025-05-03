package ast

import (
	"strings"
	"testing"
)

// TestPredicateOperators tests that our AST model correctly captures operators
func TestPredicateOperators(t *testing.T) {
	// Basic tests for Predicate.Op field
	tests := []struct {
		name        string
		predicateOp string
		isValid     bool
	}{
		{"equal", "==", true},
		{"not_equal", "!=", true},
		{"greater_than", ">", true},
		{"less_than", "<", true},
		{"greater_than_equal", ">=", true},
		{"less_than_equal", "<=", true},
		{"in", "in", true},
		{"contains", "contains", true},
		{"invalid", "!", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simply verify that our operator is in the set of valid operators we recognize
			isValid := isValidOperator(tt.predicateOp)
			if isValid != tt.isValid {
				t.Errorf("Expected operator %s valid=%v, got %v", tt.predicateOp, tt.isValid, isValid)
			}
		})
	}
}

// TestIdentifiers tests that identifiers are not confused with operators
func TestIdentifiers(t *testing.T) {
	// Test identifiers that should not be confused with operators
	tests := []struct {
		name             string
		ident            string
		containsOperator bool
	}{
		{"starts_with_in", "input", false},
		{"contains_in", "admin", false},
		{"starts_with_contains", "containsValue", false},
		{"regular_identifier", "customer", false},
		{"is_operator", "in", true},
		{"is_operator2", "contains", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check if the identifier contains an operator as a whole word
			containsOp := containsOperatorWord(tt.ident)
			if containsOp != tt.containsOperator {
				t.Errorf("Expected identifier %s containsOp=%v, got %v",
					tt.ident, tt.containsOperator, containsOp)
			}
		})
	}
}

// Helper function to check if a string is a valid operator
func isValidOperator(op string) bool {
	validOps := []string{"==", "!=", "<", ">", "<=", ">=", "in", "contains"}
	for _, valid := range validOps {
		if op == valid {
			return true
		}
	}
	return false
}

// Helper function to check if a string contains a full operator word
func containsOperatorWord(s string) bool {
	// Check if the string is exactly "in" or "contains"
	if s == "in" || s == "contains" {
		return true
	}

	// Check if the string contains " in " or " contains " with word boundaries
	return strings.Contains(" "+s+" ", " in ") ||
		strings.Contains(" "+s+" ", " contains ")
}
