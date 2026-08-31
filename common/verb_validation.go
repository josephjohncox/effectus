package common

import (
	"fmt"
	"strings"

	"github.com/josephjohncox/effectus/schema/types"
	"github.com/josephjohncox/effectus/schema/verb"
)

type verbRegistryStrictness interface {
	StrictArgs() *bool
	StrictReturn() *bool
}

func resolveStrictArgs(spec *verb.Spec, registry VerbRegistry) bool {
	if spec != nil && spec.StrictArgs != nil {
		return *spec.StrictArgs
	}
	if reg, ok := registry.(verbRegistryStrictness); ok {
		if reg.StrictArgs() != nil {
			return *reg.StrictArgs()
		}
	}
	return true
}

func resolveStrictReturn(spec *verb.Spec, registry VerbRegistry) bool {
	if spec != nil && spec.StrictReturn != nil {
		return *spec.StrictReturn
	}
	if reg, ok := registry.(verbRegistryStrictness); ok {
		if reg.StrictReturn() != nil {
			return *reg.StrictReturn()
		}
	}
	return true
}

func validateVerbArgs(spec *verb.Spec, args map[string]interface{}, registry VerbRegistry) error {
	if spec == nil {
		return nil
	}
	if !resolveStrictArgs(spec, registry) {
		return nil
	}
	if len(spec.ArgTypes) == 0 && len(spec.RequiredArgs) == 0 {
		return nil
	}

	required := spec.RequiredArgs
	if required == nil && len(spec.ArgTypes) > 0 {
		required = make([]string, 0, len(spec.ArgTypes))
		for name := range spec.ArgTypes {
			required = append(required, name)
		}
	}

	for _, name := range required {
		if _, ok := args[name]; !ok {
			return fmt.Errorf("missing required argument: %s", name)
		}
	}

	for name, value := range args {
		expectedTypeName, ok := spec.ArgTypes[name]
		if !ok {
			return fmt.Errorf("unexpected argument: %s", name)
		}
		expectedType, _ := types.ParseTypeName(expectedTypeName)
		actualType := types.InferTypeFromInterface(value)
		if expectedType != nil && !types.AreTypesCompatible(actualType, expectedType) {
			return fmt.Errorf("argument %s expected %s, got %s", name, expectedTypeName, actualType.String())
		}
	}

	return nil
}

func validateVerbReturn(spec *verb.Spec, result interface{}, registry VerbRegistry) error {
	if spec == nil {
		return nil
	}
	if !resolveStrictReturn(spec, registry) {
		return nil
	}

	expected := strings.TrimSpace(spec.ReturnType)
	if expected == "" || strings.EqualFold(expected, "unknown") || strings.EqualFold(expected, "any") {
		return nil
	}
	if strings.EqualFold(expected, "void") || strings.EqualFold(expected, "nil") {
		if result != nil {
			return fmt.Errorf("expected no return value for %s", spec.Name)
		}
		return nil
	}

	expectedType, _ := types.ParseTypeName(expected)
	actualType := types.InferTypeFromInterface(result)
	if expectedType != nil && !types.AreTypesCompatible(actualType, expectedType) {
		return fmt.Errorf("return value expected %s, got %s", expected, actualType.String())
	}

	return nil
}

// ValidateVerbArgs enforces runtime argument validation based on strict settings.
func ValidateVerbArgs(spec *verb.Spec, args map[string]interface{}, registry VerbRegistry) error {
	return validateVerbArgs(spec, args, registry)
}

// ValidateVerbReturn enforces runtime return validation based on strict settings.
func ValidateVerbReturn(spec *verb.Spec, result interface{}, registry VerbRegistry) error {
	return validateVerbReturn(spec, result, registry)
}
