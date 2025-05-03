package verb

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadFromJSON loads verb specifications from a JSON file
func (r *Registry) LoadFromJSON(filepath string) error {
	// Read the file
	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("reading verb spec file: %w", err)
	}

	// Parse verb specs
	var verbSpecs map[string]struct {
		ArgTypes    map[string]interface{} `json:"arg_types"`
		ReturnType  interface{}            `json:"return_type"`
		Capability  Capability             `json:"capability"`
		Inverse     string                 `json:"inverse,omitempty"`
		Description string                 `json:"description,omitempty"`
	}

	if err := json.Unmarshal(content, &verbSpecs); err != nil {
		return fmt.Errorf("parsing verb spec file: %w", err)
	}

	// Register each verb
	for name, jsonSpec := range verbSpecs {
		// Create the verb spec
		spec := &Spec{
			Name:        name,
			ArgTypes:    jsonSpec.ArgTypes,
			ReturnType:  jsonSpec.ReturnType,
			Capability:  jsonSpec.Capability,
			Inverse:     jsonSpec.Inverse,
			Description: jsonSpec.Description,
		}

		// Register the verb
		if err := r.RegisterVerb(spec); err != nil {
			return fmt.Errorf("registering verb %s: %w", name, err)
		}
	}

	return nil
}

// SaveToJSON saves verb specifications to a JSON file
func (r *Registry) SaveToJSON(filepath string) error {
	r.mu.RLock()

	// Create a map for JSON serialization
	jsonSpecs := make(map[string]struct {
		ArgTypes    map[string]interface{} `json:"arg_types"`
		ReturnType  interface{}            `json:"return_type"`
		Capability  Capability             `json:"capability"`
		Inverse     string                 `json:"inverse,omitempty"`
		Description string                 `json:"description,omitempty"`
	})

	// Populate the map
	for name, spec := range r.verbs {
		jsonSpecs[name] = struct {
			ArgTypes    map[string]interface{} `json:"arg_types"`
			ReturnType  interface{}            `json:"return_type"`
			Capability  Capability             `json:"capability"`
			Inverse     string                 `json:"inverse,omitempty"`
			Description string                 `json:"description,omitempty"`
		}{
			ArgTypes:    spec.ArgTypes,
			ReturnType:  spec.ReturnType,
			Capability:  spec.Capability,
			Inverse:     spec.Inverse,
			Description: spec.Description,
		}
	}

	r.mu.RUnlock()

	// Marshal to JSON
	data, err := json.MarshalIndent(jsonSpecs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling verb specs: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("writing verb spec file: %w", err)
	}

	return nil
}

// RegisterDefaults registers default verbs in the registry
func (r *Registry) RegisterDefaults() error {
	// Register common verbs

	// SendEmail - a general utility verb
	if err := r.RegisterVerb(NewSpec(
		"SendEmail",
		CapabilityModify,
		map[string]interface{}{
			"to":      "string",
			"subject": "string",
			"body":    "string",
		},
		"bool",
	).WithDescription("Sends an email to the specified recipient")); err != nil {
		return err
	}

	// LogMessage - a logging verb
	if err := r.RegisterVerb(NewSpec(
		"LogMessage",
		CapabilityNone,
		map[string]interface{}{
			"level":   "string",
			"message": "string",
		},
		"bool",
	).WithDescription("Logs a message at the specified level")); err != nil {
		return err
	}

	return nil
}

// RegisterDirectory loads all JSON files in a directory
func (r *Registry) RegisterDirectory(dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("reading verb directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only process JSON files
		name := entry.Name()
		if len(name) < 5 || name[len(name)-5:] != ".json" {
			continue
		}

		filePath := dirPath + "/" + name
		if err := r.LoadFromJSON(filePath); err != nil {
			return fmt.Errorf("loading verb file %s: %w", name, err)
		}
	}

	return nil
}
