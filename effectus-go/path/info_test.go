package path

import (
	"testing"
)

func TestSimplePath(t *testing.T) {
	tests := []struct {
		name          string
		pathStr       string
		wantNamespace string
		wantElements  int
		wantError     bool
	}{
		{
			name:          "Simple path",
			pathStr:       "customer.name",
			wantNamespace: "customer",
			wantElements:  1,
			wantError:     false,
		},
		{
			name:          "Multi-segment path",
			pathStr:       "customer.profile.email",
			wantNamespace: "customer",
			wantElements:  2,
			wantError:     false,
		},
		{
			name:          "With array index",
			pathStr:       "customer.orders[0].id",
			wantNamespace: "customer",
			wantElements:  2,
			wantError:     false,
		},
		{
			name:          "Multiple array indices",
			pathStr:       "customer.orders[1].items[2].name",
			wantNamespace: "customer",
			wantElements:  3,
			wantError:     false,
		},
		{
			name:          "Empty path",
			pathStr:       "",
			wantNamespace: "",
			wantElements:  0,
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test SimplePathFromString
			simplePath, err := SimplePathFromString(tt.pathStr)
			if (err != nil) != tt.wantError {
				t.Errorf("SimplePathFromString() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err != nil {
				return
			}

			// Test Namespace method
			if simplePath.Namespace() != tt.wantNamespace {
				t.Errorf("SimplePath.Namespace() = %v, want %v", simplePath.Namespace(), tt.wantNamespace)
			}

			// Test Elements method
			elements := simplePath.Elements()
			if len(elements) != tt.wantElements {
				t.Errorf("len(SimplePath.Elements()) = %d, want %d", len(elements), tt.wantElements)
			}

			// Test String method
			pathStr := simplePath.String()
			if pathStr != tt.pathStr && !tt.wantError {
				t.Errorf("SimplePath.String() = %v, want %v", pathStr, tt.pathStr)
			}

			// Test PathToString function
			var pathInfo PathInfo = simplePath
			pathStr = PathToString(pathInfo)
			if pathStr != tt.pathStr && !tt.wantError {
				t.Errorf("PathToString() = %v, want %v", pathStr, tt.pathStr)
			}

			// Test nil case
			if PathToString(nil) != "" {
				t.Errorf("PathToString(nil) should return empty string")
			}
		})
	}
}

func TestNewSimplePath(t *testing.T) {
	// Create a path with namespace and elements
	namespace := "customer"
	elements := []PathElement{
		{Name: "orders", Index: intPtr(0)},
		{Name: "items", Index: intPtr(2)},
		{Name: "name"},
	}

	// Create a SimplePath
	simplePath := NewSimplePath(namespace, elements)

	// Test properties
	if simplePath.Namespace() != namespace {
		t.Errorf("SimplePath.Namespace() = %v, want %v", simplePath.Namespace(), namespace)
	}

	pathElements := simplePath.Elements()
	if len(pathElements) != len(elements) {
		t.Errorf("len(SimplePath.Elements()) = %d, want %d", len(pathElements), len(elements))
	}

	// Test string representation
	expectedStr := "customer.orders[0].items[2].name"
	if simplePath.String() != expectedStr {
		t.Errorf("SimplePath.String() = %v, want %v", simplePath.String(), expectedStr)
	}
}

func TestStringToElements(t *testing.T) {
	tests := []struct {
		name          string
		pathStr       string
		wantNamespace string
		wantElements  int
		wantError     bool
	}{
		{
			name:          "Simple path",
			pathStr:       "customer.name",
			wantNamespace: "customer",
			wantElements:  1,
			wantError:     false,
		},
		{
			name:          "With array index",
			pathStr:       "customer.orders[0].id",
			wantNamespace: "customer",
			wantElements:  2,
			wantError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, elements, err := StringToElements(tt.pathStr)
			if (err != nil) != tt.wantError {
				t.Errorf("StringToElements() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err != nil {
				return
			}

			if namespace != tt.wantNamespace {
				t.Errorf("StringToElements() namespace = %v, want %v", namespace, tt.wantNamespace)
			}

			if len(elements) != tt.wantElements {
				t.Errorf("StringToElements() got %d elements, want %d", len(elements), tt.wantElements)
			}
		})
	}
}

// Helper function to create int pointer
func intPtr(i int) *int {
	return &i
}
