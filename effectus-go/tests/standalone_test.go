package tests

import (
	"testing"

	"github.com/effectus/effectus-go/eval"
)

// TestStandalone verifies the eval package can be imported properly
func TestStandalone(t *testing.T) {
	// Simple test to verify the package can be imported
	evaluator := eval.DefaultPredicateEvaluator()
	if evaluator == nil {
		t.Error("Failed to create default predicate evaluator")
	}
}
