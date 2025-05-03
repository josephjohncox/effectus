package pathutil

import (
	"testing"
)

func TestGjsonProvider(t *testing.T) {
	// Sample test data
	jsonData := `{
		"user": {
			"profile": {
				"name": "John Doe",
				"age": 30,
				"contact": {
					"email": "john@example.com",
					"phone": "+1-555-123-4567"
				}
			},
			"permissions": ["read", "write", "admin"],
			"settings": {
				"theme": "dark",
				"notifications": true
			}
		},
		"posts": [
			{
				"id": 1,
				"title": "First Post",
				"tags": ["tech", "news"]
			},
			{
				"id": 2,
				"title": "Second Post",
				"tags": ["opinion", "tech"]
			}
		]
	}`

	// Create provider
	provider := NewGjsonProviderFromJSON(jsonData)

	// Test cases
	tests := []struct {
		name     string
		path     string
		expected interface{}
		exists   bool
	}{
		{
			name:     "Simple path",
			path:     "user.profile.name",
			expected: "John Doe",
			exists:   true,
		},
		{
			name:     "Numeric value",
			path:     "user.profile.age",
			expected: int64(30),
			exists:   true,
		},
		{
			name:     "Nested path",
			path:     "user.profile.contact.email",
			expected: "john@example.com",
			exists:   true,
		},
		{
			name:     "Array index",
			path:     "posts[0].title",
			expected: "First Post",
			exists:   true,
		},
		{
			name:     "Deeper array index",
			path:     "posts[1].tags[0]",
			expected: "opinion",
			exists:   true,
		},
		{
			name:     "Boolean value",
			path:     "user.settings.notifications",
			expected: true,
			exists:   true,
		},
		{
			name:     "Non-existent path",
			path:     "user.profile.address",
			expected: nil,
			exists:   false,
		},
		{
			name:     "Out of bounds array index",
			path:     "posts[5]",
			expected: nil,
			exists:   false,
		},
	}

	// Run tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, exists := provider.Get(tt.path)

			if exists != tt.exists {
				t.Errorf("Expected exists=%v, got %v for path %s", tt.exists, exists, tt.path)
				return
			}

			if !exists {
				return // Don't compare values for non-existent paths
			}

			// For simple values, compare directly
			if value != tt.expected {
				t.Errorf("Expected %v (%T), got %v (%T) for path %s",
					tt.expected, tt.expected, value, value, tt.path)
			}
		})
	}
}

// Test the registry with gjson provider
func TestRegistryWithGjsonProvider(t *testing.T) {
	// Create two providers with different data
	userJson := `{
		"profile": {
			"name": "John Doe",
			"age": 30
		}
	}`

	systemJson := `{
		"settings": {
			"version": "1.0.0",
			"debug": true
		}
	}`

	userProvider := NewGjsonProviderFromJSON(userJson)
	systemProvider := NewGjsonProviderFromJSON(systemJson)

	// Create registry and register providers
	registry := NewRegistry()
	registry.Register("user", userProvider)
	registry.Register("system", systemProvider)

	// Test namespaced access
	tests := []struct {
		name     string
		path     string
		expected interface{}
		exists   bool
	}{
		{
			name:     "User namespace",
			path:     "user.profile.name",
			expected: "John Doe",
			exists:   true,
		},
		{
			name:     "System namespace",
			path:     "system.settings.version",
			expected: "1.0.0",
			exists:   true,
		},
		{
			name:     "Non-existent namespace",
			path:     "app.config",
			expected: nil,
			exists:   false,
		},
	}

	// Run tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, exists := registry.Get(tt.path)

			if exists != tt.exists {
				t.Errorf("Expected exists=%v, got %v for path %s", tt.exists, exists, tt.path)
				return
			}

			if !exists {
				return // Don't compare values for non-existent paths
			}

			// For simple values, compare directly
			if value != tt.expected {
				t.Errorf("Expected %v (%T), got %v (%T) for path %s",
					tt.expected, tt.expected, value, value, tt.path)
			}
		})
	}
}
