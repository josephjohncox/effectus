package eval_test

import (
	"fmt"
	"testing"
)

// TestStandalone is a simple test that doesn't import the main package
func TestStandalone(t *testing.T) {
	// Compare two simple values
	a := 5
	b := 5

	if a != b {
		t.Errorf("Expected %d to equal %d", a, b)
	}

	fmt.Println("Standalone test completed successfully")
}
