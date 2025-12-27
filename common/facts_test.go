package common

import (
	"testing"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
)

func TestBasicFacts(t *testing.T) {
	// Sample data structure
	data := map[string]interface{}{
		"app": map[string]interface{}{
			"users": []interface{}{
				map[string]interface{}{
					"id":   1,
					"name": "Alice",
					"tags": map[string]interface{}{
						"admin": true,
					},
				},
				map[string]interface{}{
					"id":   2,
					"name": "Bob",
					"tags": map[string]interface{}{
						"guest": true,
					},
				},
			},
			"settings": map[string]interface{}{
				"theme": "dark",
				"notifications": map[string]interface{}{
					"email": true,
					"push":  false,
				},
			},
		},
	}

	// Create schema with type information
	schema := NewBasicSchema()

	// Register some types
	userType := &types.Type{
		PrimType: types.TypeObject,
		Name:     "User",
	}

	settingsType := &types.Type{
		PrimType: types.TypeObject,
		Name:     "Settings",
	}
	schema.RegisterPathType("app.users", &types.Type{
		PrimType:    types.TypeList,
		ElementType: userType,
	})

	schema.RegisterPathType("app.settings", settingsType)

	// Create facts
	facts := NewBasicFacts(data, schema)

	// Test immutability
	testImmutability(t, facts, data)

	// Test path access
	testPathAccess(t, facts)

	// Test type information
	testTypeInformation(t, facts)

	// Test path parsing and resolution
	testPathParsing(t)
}

func testImmutability(t *testing.T, facts *BasicFacts, originalData map[string]interface{}) {
	// Modify the original data
	if settingsMap, ok := originalData["app"].(map[string]interface{})["settings"].(map[string]interface{}); ok {
		settingsMap["theme"] = "light" // Change theme
	}

	// Verify facts data was not changed
	value, exists := facts.Get("app.settings.theme")
	if !exists {
		t.Fatal("Expected path to exist: app.settings.theme")
	}

	if value != "dark" {
		t.Errorf("Expected theme to be 'dark', got %v", value)
	}

	// Create new facts with changes
	updatedData := deepCopyMap(originalData)
	if appMap, ok := updatedData["app"].(map[string]interface{}); ok {
		if settingsMap, ok := appMap["settings"].(map[string]interface{}); ok {
			settingsMap["theme"] = "light"
		}
	}

	updatedFacts := facts.WithData(updatedData)

	// Verify updated facts has the change
	value, exists = updatedFacts.Get("app.settings.theme")
	if !exists {
		t.Fatal("Expected path to exist in updated facts: app.settings.theme")
	}

	if value != "light" {
		t.Errorf("Expected theme to be 'light' in updated facts, got %v", value)
	}
}

func testPathAccess(t *testing.T, facts *BasicFacts) {
	tests := []struct {
		path          string
		expectedValue interface{}
		shouldExist   bool
	}{
		{"app.users[0].name", "Alice", true},
		{"app.users[1].name", "Bob", true},
		{"app.users[0].tags.admin", true, true},
		{"app.users[1].tags.guest", true, true},
		{"app.settings.theme", "dark", true},
		{"app.settings.notifications.email", true, true},
		{"app.settings.notifications.sms", nil, false},
		{"app.nonexistent", nil, false},
		{"app.users[2]", nil, false},
	}

	for _, test := range tests {
		value, exists := facts.Get(test.path)

		if exists != test.shouldExist {
			t.Errorf("Path '%s': expected exists=%v, got %v", test.path, test.shouldExist, exists)
			continue
		}

		if !test.shouldExist {
			continue
		}

		if value != test.expectedValue {
			t.Errorf("Path '%s': expected value=%v, got %v", test.path, test.expectedValue, value)
		}
	}
}

func testTypeInformation(t *testing.T, facts *BasicFacts) {
	// Get value with context to check type information
	_, result := facts.GetWithContext("app.users")

	if result == nil || !result.Exists {
		t.Fatal("Expected path to exist: app.users")
	}

	if result.Type == nil {
		t.Fatal("Expected type information for app.users")
	}

	if result.Type.PrimType != types.TypeList {
		t.Errorf("Expected list type, got %v", result.Type.PrimType)
	}

	if result.Type.ElementType == nil || result.Type.ElementType.Name != "User" {
		t.Errorf("Expected User element type, got %v", result.Type.ElementType)
	}
}

func testPathParsing(t *testing.T) {
	testPaths := []string{
		"app.users[0].name",
		"app.settings.notifications.email",
		"system.metrics[\"cpu\"].usage",
	}

	for _, pathStr := range testPaths {
		// Create a Path from the string
		path := pathutil.Path(pathStr)

		// Verify the path can be converted back to string
		backToString := path.String()

		// Parsing string path is straightforward now
		if backToString != pathStr {
			t.Errorf("String conversion mismatch: original=%s, converted=%s",
				pathStr, backToString)
		}

		// Test namespace extraction
		namespace := path.Namespace()
		if namespace == "" {
			t.Errorf("Expected non-empty namespace for path: %s", pathStr)
		}

		// Test withNamespace function
		newPath := path.WithNamespace("new")
		if newPath.Namespace() != "new" {
			t.Errorf("Expected namespace 'new', got '%s' for path: %s",
				newPath.Namespace(), pathStr)
		}
	}
}

// deepCopyMap creates a deep copy of a map
func deepCopyMap(original map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{})
	for key, value := range original {
		// Handle nested maps
		if nestedMap, ok := value.(map[string]interface{}); ok {
			copy[key] = deepCopyMap(nestedMap)
			continue
		}

		// Handle nested slices
		if nestedSlice, ok := value.([]interface{}); ok {
			copySlice := make([]interface{}, len(nestedSlice))
			for i, item := range nestedSlice {
				if itemMap, ok := item.(map[string]interface{}); ok {
					copySlice[i] = deepCopyMap(itemMap)
				} else {
					copySlice[i] = item
				}
			}
			copy[key] = copySlice
			continue
		}

		// For other types, just copy the value
		copy[key] = value
	}
	return copy
}
