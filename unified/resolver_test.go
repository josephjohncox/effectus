package unified

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveManifestFileRegistry(t *testing.T) {
	root := t.TempDir()
	registryDir := filepath.Join(root, "registry")

	bundles := []Bundle{
		{
			Name:        "fraud-core",
			Version:     "1.2.3",
			Description: "older",
			CreatedAt:   time.Now(),
		},
		{
			Name:        "fraud-core",
			Version:     "1.4.0",
			Description: "latest",
			CreatedAt:   time.Now(),
		},
	}

	for _, bundle := range bundles {
		bundleDir := filepath.Join(registryDir, bundle.Name, bundle.Version)
		if err := os.MkdirAll(bundleDir, 0755); err != nil {
			t.Fatalf("Failed to create bundle dir: %v", err)
		}
		bundlePath := filepath.Join(bundleDir, "bundle.json")
		if err := SaveBundle(&bundle, bundlePath); err != nil {
			t.Fatalf("Failed to save bundle: %v", err)
		}
	}

	latestPath := filepath.Join(registryDir, "fraud-core", "1.4.0", "bundle.json")
	latestData, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatalf("Failed to read latest bundle: %v", err)
	}
	checksum := checksumBytes(latestData)

	manifest := &ExtensionManifest{
		Name:    "test",
		Version: "0.1.0",
		Registries: []RegistryConfig{
			{
				Name:    "local",
				Base:    registryDir,
				Type:    "file",
				Default: true,
			},
		},
		Bundles: []BundleRequirement{
			{
				Name:     "fraud-core",
				Version:  "^1.2.0",
				Registry: "local",
				Checksum: checksum,
			},
		},
	}

	resolved, err := ResolveManifest(manifest, root, ResolverOptions{AllowFile: true, VerifyChecksum: true})
	if err != nil {
		t.Fatalf("ResolveManifest failed: %v", err)
	}

	if len(resolved) != 1 {
		t.Fatalf("Expected 1 resolved bundle, got %d", len(resolved))
	}

	if resolved[0].Version != "1.4.0" {
		t.Fatalf("Expected version 1.4.0, got %s", resolved[0].Version)
	}

	if resolved[0].Checksum != checksum {
		t.Fatalf("Checksum mismatch: expected %s, got %s", checksum, resolved[0].Checksum)
	}
}

func TestSemverConstraints(t *testing.T) {
	cases := []struct {
		Version    string
		Constraint string
		Expected   bool
	}{
		{"1.2.3", "1.2.3", true},
		{"1.2.3", ">=1.2.0 <2.0.0", true},
		{"1.2.3", "^1.2.0", true},
		{"1.2.3", "~1.2.0", true},
		{"2.0.0", "^1.2.0", false},
		{"1.3.0", "1.2.x", false},
		{"1.2.5", "1.2.x", true},
	}

	for _, c := range cases {
		ok, err := satisfiesConstraint(c.Version, c.Constraint)
		if err != nil {
			t.Fatalf("Constraint error for %s %s: %v", c.Version, c.Constraint, err)
		}
		if ok != c.Expected {
			t.Fatalf("Expected %v for %s %s, got %v", c.Expected, c.Version, c.Constraint, ok)
		}
	}
}
