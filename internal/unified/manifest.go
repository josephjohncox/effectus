package unified

import (
	"encoding/json"
	"fmt"
	"os"
)

// ExtensionManifest describes a set of bundle dependencies.
type ExtensionManifest struct {
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description,omitempty"`
	Effectus    string              `json:"effectus,omitempty"`
	Registries  []RegistryConfig    `json:"registries,omitempty"`
	Bundles     []BundleRequirement `json:"bundles"`
}

// RegistryConfig defines where bundles are resolved.
type RegistryConfig struct {
	Name    string `json:"name"`
	Base    string `json:"base"`
	Type    string `json:"type,omitempty"` // "oci" (default) or "file"
	Default bool   `json:"default,omitempty"`
}

// BundleRequirement describes a bundle dependency.
type BundleRequirement struct {
	Name          string         `json:"name"`
	Version       string         `json:"version"`
	Registry      string         `json:"registry,omitempty"`
	OCI           string         `json:"oci,omitempty"`
	File          string         `json:"file,omitempty"`
	Checksum      string         `json:"checksum,omitempty"`
	Optional      bool           `json:"optional,omitempty"`
	Compatibility *Compatibility `json:"compatibility,omitempty"`
}

// Compatibility describes compatibility constraints for a bundle.
type Compatibility struct {
	Effectus string            `json:"effectus,omitempty"`
	Bundles  map[string]string `json:"bundles,omitempty"`
}

// LoadExtensionManifest loads a manifest from disk.
func LoadExtensionManifest(path string) (*ExtensionManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var manifest ExtensionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	return &manifest, nil
}
