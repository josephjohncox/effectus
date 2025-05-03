// unified/bundle_test.go
package unified

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/effectus/effectus-go/schema/registry"
)

func TestBundleBuilder(t *testing.T) {
	// Create temporary directories for test
	tmpDir := t.TempDir()
	schemaDir := filepath.Join(tmpDir, "schema")
	verbDir := filepath.Join(tmpDir, "verbs")
	rulesDir := filepath.Join(tmpDir, "rules")

	// Create test directories
	for _, dir := range []string{schemaDir, verbDir, rulesDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create test directory %s: %v", dir, err)
		}
	}

	// Create a simple schema file with proper format for JSONSchemaLoader
	schemaContent := `{
		"types": {},
		"facts": {
			"name": {
				"type": "string"
			},
			"age": {
				"type": "integer"
			},
			"email": {
				"type": "string"
			}
		}
	}`
	schemaFile := filepath.Join(schemaDir, "user.json")
	if err := os.WriteFile(schemaFile, []byte(schemaContent), 0644); err != nil {
		t.Fatalf("Failed to write schema file: %v", err)
	}

	// Create a simple verb file with proper format for Registry.LoadFromJSON
	verbContent := `{
		"sendEmail": {
			"arg_types": {
				"to": {"primType": 1, "name": "string"},
				"subject": {"primType": 1, "name": "string"},
				"body": {"primType": 1, "name": "string"}
			},
			"return_type": {"primType": 1, "name": "string"},
			"capability": 1,
			"description": "Sends an email"
		}
	}`
	verbFile := filepath.Join(verbDir, "email.json")
	if err := os.WriteFile(verbFile, []byte(verbContent), 0644); err != nil {
		t.Fatalf("Failed to write verb file: %v", err)
	}

	// Create a simple rule file with correct Effectus syntax
	ruleContent := `rule "USER_CHECK" priority 10 {
		when {
			user.age > 18
		}
		then {
			sendEmail(to: user.email, subject: "Welcome", body: "Welcome to the service!")
		}
	}`
	ruleFile := filepath.Join(rulesDir, "user_check.eff")
	if err := os.WriteFile(ruleFile, []byte(ruleContent), 0644); err != nil {
		t.Fatalf("Failed to write rule file: %v", err)
	}

	// Create a bundle builder
	builder := NewBundleBuilder("test-bundle", "1.0.0")
	builder.WithDescription("Test bundle")
	builder.WithSchemaDir(schemaDir)
	builder.WithVerbDir(verbDir)
	builder.WithRulesDir(rulesDir)
	builder.WithPIIMasks([]string{"user.email"})

	// Register schema loaders
	jsonLoader := registry.NewJSONSchemaLoader("user")
	// ProtoSchemaLoader is no longer available, using only JSON loader for tests
	// protoLoader := registry.NewProtoSchemaLoader("")
	builder.RegisterSchemaLoader(jsonLoader)
	// builder.RegisterSchemaLoader(protoLoader)

	// Skip rule compilation for testing
	builder.SkipRuleCompilation()

	// Build the bundle
	bundle, err := builder.Build()
	if err != nil {
		t.Fatalf("Failed to build bundle: %v", err)
	}

	// Verify bundle contents
	if bundle.Name != "test-bundle" {
		t.Errorf("Expected bundle name 'test-bundle', got '%s'", bundle.Name)
	}

	if bundle.Version != "1.0.0" {
		t.Errorf("Expected bundle version '1.0.0', got '%s'", bundle.Version)
	}

	if bundle.Description != "Test bundle" {
		t.Errorf("Expected bundle description 'Test bundle', got '%s'", bundle.Description)
	}

	if len(bundle.SchemaFiles) != 1 {
		t.Errorf("Expected 1 schema file, got %d", len(bundle.SchemaFiles))
	}

	if len(bundle.VerbFiles) != 1 {
		t.Errorf("Expected 1 verb file, got %d", len(bundle.VerbFiles))
	}

	if len(bundle.RuleFiles) != 1 {
		t.Errorf("Expected 1 rule file, got %d", len(bundle.RuleFiles))
	}

	if len(bundle.PIIMasks) != 1 || bundle.PIIMasks[0] != "user.email" {
		t.Errorf("Expected PIIMasks to contain 'user.email', got %v", bundle.PIIMasks)
	}

	// Create a bundle file
	bundleFile := filepath.Join(tmpDir, "bundle.json")
	if err := SaveBundle(bundle, bundleFile); err != nil {
		t.Fatalf("Failed to save bundle: %v", err)
	}

	// Load the bundle
	loadedBundle, err := LoadBundle(bundleFile)
	if err != nil {
		t.Fatalf("Failed to load bundle: %v", err)
	}

	// Verify loaded bundle
	if loadedBundle.Name != bundle.Name {
		t.Errorf("Loaded bundle name mismatch: expected '%s', got '%s'", bundle.Name, loadedBundle.Name)
	}

	if loadedBundle.Version != bundle.Version {
		t.Errorf("Loaded bundle version mismatch: expected '%s', got '%s'", bundle.Version, loadedBundle.Version)
	}

	if loadedBundle.VerbHash != bundle.VerbHash {
		t.Errorf("Loaded bundle verb hash mismatch: expected '%s', got '%s'", bundle.VerbHash, loadedBundle.VerbHash)
	}
}

func TestOCIBundlePusherAndPuller(t *testing.T) {
	// Skip this test by default since it requires OCI registry
	if os.Getenv("TEST_OCI") != "true" {
		t.Skip("Skipping OCI test; set TEST_OCI=true to run")
	}

	// Create a simple bundle for testing
	bundle := &Bundle{
		Name:        "test-oci-bundle",
		Version:     "1.0.0",
		Description: "Test OCI bundle",
		CreatedAt:   time.Now(),
		VerbHash:    "abc123",
		SchemaFiles: []string{"schema1.json"},
		VerbFiles:   []string{"verb1.json"},
		RuleFiles:   []string{"rule1.eff"},
	}

	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Create test directories
	schemaDir := filepath.Join(tmpDir, "schema")
	verbDir := filepath.Join(tmpDir, "verbs")
	rulesDir := filepath.Join(tmpDir, "rules")
	outputDir := filepath.Join(tmpDir, "output")

	for _, dir := range []string{schemaDir, verbDir, rulesDir, outputDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create test directory %s: %v", dir, err)
		}
	}

	// Create test files
	files := map[string]string{
		filepath.Join(schemaDir, "schema1.json"): `{"test": "schema"}`,
		filepath.Join(verbDir, "verb1.json"):     `{"test": "verb"}`,
		filepath.Join(rulesDir, "rule1.eff"):     `rule test {}`,
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write test file %s: %v", path, err)
		}
	}

	// Create a pusher
	imageRef := "localhost:5000/test-bundle:v1.0.0" // Local registry for testing
	pusher := NewOCIBundlePusher(bundle)
	pusher.WithSchemaDir(schemaDir)
	pusher.WithVerbDir(verbDir)
	pusher.WithRulesDir(rulesDir)

	// Push the bundle
	if err := pusher.Push(imageRef); err != nil {
		t.Fatalf("Failed to push bundle: %v", err)
	}

	// Create a puller
	puller := NewOCIBundlePuller(outputDir)

	// Pull the bundle
	pulledBundle, err := puller.Pull(imageRef)
	if err != nil {
		t.Fatalf("Failed to pull bundle: %v", err)
	}

	// Verify pulled bundle
	if pulledBundle.Name != bundle.Name {
		t.Errorf("Pulled bundle name mismatch: expected '%s', got '%s'", bundle.Name, pulledBundle.Name)
	}

	if pulledBundle.Version != bundle.Version {
		t.Errorf("Pulled bundle version mismatch: expected '%s', got '%s'", bundle.Version, pulledBundle.Version)
	}

	if pulledBundle.VerbHash != bundle.VerbHash {
		t.Errorf("Pulled bundle verb hash mismatch: expected '%s', got '%s'", bundle.VerbHash, pulledBundle.VerbHash)
	}

	// Verify files were extracted
	for _, file := range []string{"schema1.json", "verb1.json", "rule1.eff"} {
		expectedDir := ""
		switch {
		case file == "schema1.json":
			expectedDir = "schema"
		case file == "verb1.json":
			expectedDir = "verbs"
		case file == "rule1.eff":
			expectedDir = "rules"
		}

		path := filepath.Join(outputDir, expectedDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected file %s to exist after pull", path)
		}
	}
}
