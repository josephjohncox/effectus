// schema/verb_loader.go
package schema

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadVerbsFromJSON loads verb specifications from a JSON file
func (vr *VerbRegistry) LoadVerbsFromJSON(filepath string) error {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("reading verb spec file: %w", err)
	}

	var verbSpecs map[string]struct {
		ArgTypes    map[string]Type `json:"arg_types"`
		ReturnType  Type            `json:"return_type"`
		Capability  VerbCapability  `json:"capability"`
		Inverse     string          `json:"inverse,omitempty"`
		Description string          `json:"description,omitempty"`
	}

	if err := json.Unmarshal(content, &verbSpecs); err != nil {
		return fmt.Errorf("parsing verb spec file: %w", err)
	}

	// Register each verb
	for name, jsonSpec := range verbSpecs {
		// Convert arg types to proper map
		argTypes := make(map[string]*Type)
		for argName, argType := range jsonSpec.ArgTypes {
			typeCopy := argType
			argTypes[argName] = &typeCopy
		}

		returnTypeCopy := jsonSpec.ReturnType

		// Create the verb spec
		spec := &VerbSpec{
			Name:        name,
			ArgTypes:    argTypes,
			ReturnType:  &returnTypeCopy,
			Capability:  jsonSpec.Capability,
			Inverse:     jsonSpec.Inverse,
			Description: jsonSpec.Description,
		}

		// Register the verb
		if err := vr.RegisterVerb(spec); err != nil {
			return fmt.Errorf("registering verb %s: %w", name, err)
		}
	}

	return nil
}
