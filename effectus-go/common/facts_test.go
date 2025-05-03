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
	namespace, elements, err := pathutil.ParsePath("app.users")
	if err != nil {
		t.Fatalf("Failed to parse path: %v", err)
	}
	schema.RegisterPathType(pathutil.NewPath(namespace, elements), &types.Type{
		PrimType: types.TypeList,
		ElementType: userType,
	})

	namespace, elements, err = pathutil.ParsePath("app.settings")
	if err != nil {
		t.Fatalf("Failed to parse path: %v", err)
	}
	schema.RegisterPathType(pathutil.NewPath(namespace, elements), settingsType)

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
	namespace, elements, err := pathutil.ParsePath("app.settings.theme")
	if err != nil {
		t.Fatalf("Failed to parse path: %v", err)
	}
	value, exists := facts.Get(pathutil.NewPath(namespace, elements))
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
	namespace, elements, err = pathutil.ParsePath("app.settings.theme")
	if err != nil {
		t.Fatalf("Failed to parse path: %v", err)
	}
	value, exists = updatedFacts.Get(pathutil.NewPath(namespace, elements))
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
		namespace, elements, err := pathutil.ParsePath(test.path)
		if err != nil {
			t.Fatalf("Failed to parse path: %v", err)
		}
		value, exists := facts.Get(pathutil.NewPath(namespace, elements))

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
	namespace, elements, err := pathutil.ParsePath("app.users")
	if err != nil {
		t.Fatalf("Failed to parse path: %v", err)
	}
	_, result := facts.GetWithContext(pathutil.NewPath(namespace, elements))

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
		path, err := pathutil.ParseString(pathStr)

		if err != nil {
			t.Errorf("Failed to parse path '%s': %v", pathStr, err)
			continue
		}

		// Verify the path can be converted back to string
		backToString := path.String()

		// The exact format may be slightly different (e.g., with spaces),
		// but parsing it again should give the same structure
		reparsed, err := pathutil.ParseString(backToString)

		if err != nil {
			t.Errorf("Failed to parse formatted path '%s': %v", backToString, err)
			continue
		}

		if len(path.Elements) != len(reparsed.Elements) {
			t.Errorf("Element count mismatch: original=%d, reparsed=%d",
				len(path.Elements), len(reparsed.Elements))
		}
	}
}
