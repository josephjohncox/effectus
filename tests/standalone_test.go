package tests

import (
	"testing"

	"github.com/josephjohncox/effectus/schema"
)

// TestStandalone verifies the schema registry can be created properly
func TestStandalone(t *testing.T) {
	// Simple test to verify the package can be imported
	registry := schema.NewRegistry()
	if registry == nil {
		t.Error("Failed to create schema registry")
	}
}
